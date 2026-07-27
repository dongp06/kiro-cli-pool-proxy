package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"kiro-cli-pool-proxy/auth"
	"kiro-cli-pool-proxy/config"
)

// newAccountID mints a pool-local account ID (matches apiAddAccount's scheme).
func newAccountID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return "acc-" + hex.EncodeToString(b)
}

// persistLoginAccount assigns an ID, enables the account, resolves the profile
// ARN (best-effort — required for chat rewrite + quota), saves config, and
// reloads the pool. It returns the stored account.
func (a *AdminHandler) persistLoginAccount(acc config.Account) config.Account {
	if strings.TrimSpace(acc.ID) == "" {
		acc.ID = newAccountID()
	}
	acc.Enabled = true
	// api_key (ksk_) accounts are region-bound and authenticate by key alone;
	// they have no profileArn and ResolveProfileArn would fail. Skip resolution.
	if acc.AuthMethod != "api_key" && strings.TrimSpace(acc.ProfileArn) == "" {
		if arn, err := auth.ResolveProfileArn(&acc); err == nil && arn != "" {
			acc.ProfileArn = arn
			if acc.Region == "" {
				acc.Region = RegionFromProfileArn(arn)
			}
		} else if err != nil {
			log.Printf("[login] resolve profileArn for %s (%s): %v", acc.ID, acc.Email, err)
		}
	}
	a.cfg.AddAccount(acc)
	a.cfg.Save()
	a.pool.Reload()
	a.publishAccounts()
	return acc
}

// expiresAtFromNow converts an expires-in (seconds) into an absolute unix
// timestamp; returns 0 when the value is non-positive.
func expiresAtFromNow(expiresIn int) int64 {
	if expiresIn <= 0 {
		return 0
	}
	return time.Now().Unix() + int64(expiresIn)
}

// --- IAM Identity Center (IdC) SSO ---

func (a *AdminHandler) apiStartIamSso(w http.ResponseWriter, r *http.Request) {
	var body struct {
		StartUrl string `json:"startUrl"`
		Region   string `json:"region"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if strings.TrimSpace(body.StartUrl) == "" {
		writeJSON(w, 400, map[string]string{"error": "startUrl is required"})
		return
	}
	sessionID, authorizeUrl, expiresIn, err := auth.StartIamSsoLogin(strings.TrimSpace(body.StartUrl), strings.TrimSpace(body.Region))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{
		"sessionId":    sessionID,
		"authorizeUrl": authorizeUrl,
		"expiresIn":    expiresIn,
	})
}

func (a *AdminHandler) apiCompleteIamSso(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionId   string `json:"sessionId"`
		CallbackUrl string `json:"callbackUrl"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	accessToken, refreshToken, clientID, clientSecret, region, expiresIn, err := auth.CompleteIamSsoLogin(
		strings.TrimSpace(body.SessionId), strings.TrimSpace(body.CallbackUrl))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	email, userID, _ := auth.GetUserInfo(accessToken)
	acc := config.Account{
		Email:        email,
		UserId:       userID,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		AuthMethod:   "idc",
		Region:       region,
		ExpiresAt:    expiresAtFromNow(expiresIn),
		MachineId:    config.GenerateMachineId(),
	}
	acc = a.persistLoginAccount(acc)
	writeJSON(w, 200, map[string]any{
		"success": true,
		"account": map[string]string{"id": acc.ID, "email": acc.Email},
	})
}

// --- AWS Builder ID (device code) ---

func (a *AdminHandler) apiStartBuilderId(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Region string `json:"region"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	session, err := auth.StartBuilderIdLogin(strings.TrimSpace(body.Region))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{
		"sessionId":       session.ID,
		"userCode":        session.UserCode,
		"verificationUri": session.VerificationUri,
		"interval":        session.Interval,
	})
}

func (a *AdminHandler) apiPollBuilderId(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionId string `json:"sessionId"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	accessToken, refreshToken, clientID, clientSecret, region, expiresIn, status, err := auth.PollBuilderIdAuth(
		strings.TrimSpace(body.SessionId))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	if status == "pending" || status == "slow_down" {
		resp := map[string]any{"success": true, "completed": false, "status": status}
		if s := auth.GetBuilderIdSession(strings.TrimSpace(body.SessionId)); s != nil {
			resp["interval"] = s.Interval
		}
		writeJSON(w, 200, resp)
		return
	}

	email, userID, _ := auth.GetUserInfo(accessToken)
	acc := config.Account{
		Email:        email,
		UserId:       userID,
		Provider:     "BuilderId",
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		AuthMethod:   "idc",
		Region:       region,
		ExpiresAt:    expiresAtFromNow(expiresIn),
		MachineId:    config.GenerateMachineId(),
	}
	acc = a.persistLoginAccount(acc)
	writeJSON(w, 200, map[string]any{
		"success":   true,
		"completed": true,
		"account":   map[string]string{"id": acc.ID, "email": acc.Email},
	})
}

// --- SSO Token import (x-amz-sso_authn bearer) ---

func (a *AdminHandler) apiImportSsoToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BearerToken string `json:"bearerToken"`
		Region      string `json:"region"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	region := strings.TrimSpace(body.Region)
	tokens := splitTokens(body.BearerToken)
	if len(tokens) == 0 {
		writeJSON(w, 400, map[string]string{"error": "bearerToken is required"})
		return
	}

	accounts := make([]map[string]string, 0, len(tokens))
	errs := make([]string, 0)
	for _, tok := range tokens {
		accessToken, refreshToken, clientID, clientSecret, expiresIn, err := auth.ImportFromSsoToken(tok, region)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		email, userID, _ := auth.GetUserInfo(accessToken)
		acc := config.Account{
			Email:        email,
			UserId:       userID,
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			ClientID:     clientID,
			ClientSecret: clientSecret,
			AuthMethod:   "idc",
			Region:       region,
			ExpiresAt:    expiresAtFromNow(expiresIn),
			MachineId:    config.GenerateMachineId(),
		}
		acc = a.persistLoginAccount(acc)
		accounts = append(accounts, map[string]string{"id": acc.ID, "email": acc.Email})
	}

	writeJSON(w, 200, map[string]any{
		"success":  len(accounts) > 0,
		"accounts": accounts,
		"errors":   errs,
	})
}

// splitTokens splits a newline-separated batch of bearer tokens, trimming
// whitespace and dropping blanks.
func splitTokens(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// --- Microsoft Enterprise SSO (external_idp / AzureAD) ---

func (a *AdminHandler) apiStartMicrosoftSso(w http.ResponseWriter, r *http.Request) {
	sessionID, authorizeUrl, expiresIn, err := auth.StartMicrosoftSSOLogin()
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{
		"sessionId":    sessionID,
		"authorizeUrl": authorizeUrl,
		"expiresIn":    expiresIn,
		"stage":        "kiro",
	})
}

func (a *AdminHandler) apiCompleteMicrosoftSso(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionId   string `json:"sessionId"`
		CallbackUrl string `json:"callbackUrl"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	progress, err := auth.ContinueMicrosoftSSOLogin(strings.TrimSpace(body.SessionId), strings.TrimSpace(body.CallbackUrl))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	// First leg: return the Microsoft provider authorization URL.
	if progress.AuthorizationURL != "" {
		writeJSON(w, 200, map[string]any{
			"success":      true,
			"stage":        "microsoft",
			"authorizeUrl": progress.AuthorizationURL,
		})
		return
	}

	// Second leg: a credential was produced. Persist it as an external_idp account.
	res := progress.Result
	if res == nil {
		writeJSON(w, 400, map[string]string{"error": "Microsoft SSO did not return a credential"})
		return
	}
	acc := config.Account{
		Email:         res.Email,
		UserId:        res.UserID,
		Provider:      auth.MicrosoftSSOProvider,
		AccessToken:   res.AccessToken,
		RefreshToken:  res.RefreshToken,
		ClientID:      res.ClientID,
		AuthMethod:    auth.MicrosoftSSOAuthMethod,
		TokenEndpoint: res.TokenEndpoint,
		IssuerURL:     res.IssuerURL,
		Scopes:        res.Scopes,
		ExpiresAt:     res.ExpiresAt,
		MachineId:     config.GenerateMachineId(),
	}
	acc = a.persistLoginAccount(acc)
	writeJSON(w, 200, map[string]any{
		"success": true,
		"stage":   "complete",
		"account": map[string]string{"id": acc.ID, "email": acc.Email},
	})
}

func (a *AdminHandler) apiCancelMicrosoftSso(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionId string `json:"sessionId"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	auth.CancelMicrosoftSSOLogin(strings.TrimSpace(body.SessionId))
	writeJSON(w, 200, map[string]bool{"success": true})
}

// --- Kiro API key (ksk_) import ---

// apiImportApiKey imports a Kiro API key (ksk_...) as an api_key account.
// The key is region-bound; the region is probed via the control-plane
// getUsageLimits GET form (auth.ImportKiroAPIKey). No profileArn is resolved.
func (a *AdminHandler) apiImportApiKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ApiKey string `json:"apiKey"`
		Region string `json:"region"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	key := strings.TrimSpace(body.ApiKey)
	if key == "" {
		writeJSON(w, 400, map[string]string{"error": "apiKey is required"})
		return
	}
	if !auth.IsKiroAPIKey(key) {
		writeJSON(w, 400, map[string]string{"error": "not a Kiro API key (expected " + auth.KiroAPIKeyPrefix + " prefix)"})
		return
	}

	acc, err := auth.ImportKiroAPIKey(key, strings.TrimSpace(body.Region))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	acc.MachineId = config.GenerateMachineId()

	stored := a.persistLoginAccount(*acc)
	writeJSON(w, 200, map[string]any{
		"success": true,
		"account": map[string]string{"id": stored.ID, "email": stored.Email, "region": stored.Region},
	})
}
