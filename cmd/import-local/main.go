// Command import-local reads credentials from a local kiro-cli SQLite database
// (~/.local/share/kiro-cli/data.sqlite3) and prints an account JSON block ready
// to paste into the proxy config.json.
//
// Schema reverse-engineered from kiro-cli 2.12.2 (see REVERSE_ENGINEERING.md):
//   auth_kv["kirocli:odic:token"]              -> access/refresh token, expiry, region, start_url
//   auth_kv["kirocli:odic:device-registration"] -> client_id, client_secret
//   auth_kv["kirocli:social:token"]            -> social token
//   auth_kv["kirocli:external-idp:token"]      -> external IdP token
//
// This tool uses a minimal pure-Go SQLite page reader (no cgo) to extract the
// two known keys. For robustness it shells out to `sqlite3` if available.
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

func main() {
	dbPath := defaultDBPath()
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}

	if _, err := os.Stat(dbPath); err != nil {
		fmt.Fprintf(os.Stderr, "Database not found: %s\n", dbPath)
		fmt.Fprintf(os.Stderr, "Usage: import-local [path/to/data.sqlite3]\n")
		os.Exit(1)
	}

	kv, err := readAuthKV(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read auth_kv: %v\n", err)
		os.Exit(1)
	}

	acc, err := buildAccount(kv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to build account: %v\n", err)
		os.Exit(1)
	}

	out, _ := json.MarshalIndent(acc, "", "  ")
	fmt.Println("// Paste this into the \"accounts\" array of config.json:")
	fmt.Println(string(out))
}

func defaultDBPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "kiro-cli", "data.sqlite3")
}

// readAuthKV extracts all auth_kv rows via a pure-Go SQLite driver (no cgo,
// no external sqlite3 binary). Returns key->rawJSON map.
func readAuthKV(dbPath string) (map[string]string, error) {
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT key, value FROM auth_kv")
	if err != nil {
		return nil, fmt.Errorf("query auth_kv: %w", err)
	}
	defer rows.Close()

	kv := make(map[string]string)
	for rows.Next() {
		var k string
		var v []byte
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		kv[k] = string(v)
	}
	return kv, rows.Err()
}

// account mirrors the proxy config.Account fields we can populate.
type account struct {
	ID           string `json:"id"`
	Email        string `json:"email,omitempty"`
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	AuthMethod   string `json:"authMethod"`
	Region       string `json:"region,omitempty"`
	ProfileArn   string `json:"profileArn,omitempty"`
	ExpiresAt    int64  `json:"expiresAt,omitempty"`
	Enabled      bool   `json:"enabled"`
}

func buildAccount(kv map[string]string) (*account, error) {
	// Try odic (IdC / Builder ID) first.
	tokenRaw, ok := kv["kirocli:odic:token"]
	authMethod := "idc"
	if !ok {
		if s, ok2 := kv["kirocli:social:token"]; ok2 {
			tokenRaw = s
			authMethod = "social"
		} else if e, ok3 := kv["kirocli:external-idp:token"]; ok3 {
			tokenRaw = e
			authMethod = "external_idp"
		} else {
			return nil, fmt.Errorf("no known token key found in auth_kv")
		}
	}

	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresAt    string `json:"expires_at"`
		Region       string `json:"region"`
		StartURL     string `json:"start_url"`
	}
	if err := json.Unmarshal([]byte(tokenRaw), &tok); err != nil {
		return nil, fmt.Errorf("parse token json: %w", err)
	}

	acc := &account{
		ID:           "imported-" + time.Now().Format("20060102-150405"),
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		AuthMethod:   authMethod,
		Region:       tok.Region,
		Enabled:      true,
	}

	// Parse expires_at (RFC3339) → unix seconds.
	if tok.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, tok.ExpiresAt); err == nil {
			acc.ExpiresAt = t.Unix()
		}
	}
	if acc.Region == "" {
		acc.Region = "us-east-1"
	}

	// Merge device-registration for clientId/clientSecret (IdC).
	if regRaw, ok := kv["kirocli:odic:device-registration"]; ok {
		var reg struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		}
		if err := json.Unmarshal([]byte(regRaw), &reg); err == nil {
			acc.ClientID = reg.ClientID
			acc.ClientSecret = reg.ClientSecret
		}
	}

	fmt.Fprintf(os.Stderr, "Imported %s account (region=%s, expires=%s)\n",
		authMethod, acc.Region, tok.ExpiresAt)
	if acc.ProfileArn == "" {
		fmt.Fprintf(os.Stderr, "⚠️  profileArn is empty — the proxy will need to resolve it,\n")
		fmt.Fprintf(os.Stderr, "    or add it manually (arn:aws:codewhisperer:REGION:...:profile/...)\n")
	}
	return acc, nil
}
