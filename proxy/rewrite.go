package proxy

import (
	"net/url"
	"regexp"
	"strings"
)

// hostFromURL extracts the host component from an absolute URL, returning ""
// when the URL cannot be parsed.
func hostFromURL(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return u.Host
	}
	return ""
}

// apiKeyChatURL returns the full chat (GenerateAssistantResponse) URL for a
// Kiro API key (ksk_) account. API keys are NOT accepted at runtime.*.kiro.dev;
// they must hit the CodeWhisperer data-plane at q.{region}.amazonaws.com with
// the /generateAssistantResponse path and Content-Type application/json.
// (Ported from Kiro-Go apiKeyRuntimeEndpoint + regionalizeURLForRegion.)
func apiKeyChatURL(region string) string {
	region = strings.TrimSpace(region)
	if region == "" || region == "us-east-1" {
		return "https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse"
	}
	return "https://q." + region + ".amazonaws.com/generateAssistantResponse"
}

// runtimeHostForRegion returns the exact runtime (data-plane / chat) host.
// Table confirmed from kiro-cli binary endpoints.rs. GovCloud uses kiro.dev;
// ISO regions have no runtime kiro.dev host (fall back to us-east-1).
func runtimeHostForRegion(region string) string {
	switch region {
	case "", "us-east-1":
		return "runtime.us-east-1.kiro.dev"
	case "eu-central-1":
		return "runtime.eu-central-1.kiro.dev"
	case "us-gov-east-1":
		return "runtime.us-gov-east-1.kiro.dev"
	case "us-gov-west-1":
		return "runtime.us-gov-west-1.kiro.dev"
	default:
		// Unknown region: try regional kiro.dev, upstream will reject if invalid.
		return "runtime." + region + ".kiro.dev"
	}
}

// managementHostForRegion returns the exact management (control-plane) host.
// Table confirmed from kiro-cli binary endpoints.rs (incl. GovCloud/ISO).
func managementHostForRegion(region string) string {
	switch region {
	case "", "us-east-1":
		return "management.us-east-1.kiro.dev"
	case "eu-central-1":
		return "management.eu-central-1.kiro.dev"
	case "us-gov-east-1":
		return "management.us-gov-east-1.kiro.dev"
	case "us-gov-west-1":
		return "management.us-gov-west-1.kiro.dev"
	case "us-iso-east-1":
		return "kiro-management.us-iso-east-1.c2s.ic.gov"
	case "us-isob-east-1":
		return "kiro-management.us-isob-east-1.sc2s.sgov.gov"
	case "us-isof-south-1":
		return "kiro-management.us-isof-south-1.csp.hci.ic.gov"
	case "us-isof-east-1":
		return "kiro-management.us-isof-east-1.csp.hci.ic.gov"
	default:
		return "management." + region + ".kiro.dev"
	}
}

// profileArnRegex matches "profileArn":"..." in JSON body.
var profileArnRegex = regexp.MustCompile(`"profileArn"\s*:\s*"[^"]*"`)

// RemoveProfileArn deletes the first profileArn member from a JSON body. API-key
// (ksk_) accounts authenticate by key alone and the CodeWhisperer data-plane
// rejects a stray/placeholder profileArn — upstream sends the field omitempty.
// It removes exactly one adjacent comma (trailing preferred, else leading) so
// the surrounding object stays valid JSON.
func RemoveProfileArn(body []byte) []byte {
	loc := profileArnRegex.FindIndex(body)
	if loc == nil {
		return body
	}
	start, end := loc[0], loc[1]

	// Absorb a trailing comma (", " form): `"profileArn":"x",` → removed.
	i := end
	for i < len(body) && (body[i] == ' ' || body[i] == '\t' || body[i] == '\n' || body[i] == '\r') {
		i++
	}
	if i < len(body) && body[i] == ',' {
		end = i + 1
	} else {
		// No trailing comma (last member): absorb a leading comma instead.
		j := start - 1
		for j >= 0 && (body[j] == ' ' || body[j] == '\t' || body[j] == '\n' || body[j] == '\r') {
			j--
		}
		if j >= 0 && body[j] == ',' {
			start = j
		}
	}

	out := make([]byte, 0, len(body)-(end-start))
	out = append(out, body[:start]...)
	out = append(out, body[end:]...)
	return out
}

// RewriteProfileArn replaces the profileArn value in the request body.
// Uses regex replacement to avoid full JSON parse/re-marshal.
func RewriteProfileArn(body []byte, newArn string) []byte {
	if newArn == "" {
		return body
	}
	replacement := `"profileArn":"` + newArn + `"`
	return profileArnRegex.ReplaceAll(body, []byte(replacement))
}

// RegionFromProfileArn extracts the AWS region from a profile ARN.
// Format: arn:aws:codewhisperer:{region}:{account}:profile/{id}
func RegionFromProfileArn(arn string) string {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 || parts[0] != "arn" {
		return ""
	}
	return strings.TrimSpace(parts[3])
}

// TokenTypeHeader returns the value for the "tokentype" header based on auth method.
func TokenTypeHeader(authMethod string) string {
	switch authMethod {
	case "social", "external_idp":
		return "EXTERNAL_IDP"
	case "api_key":
		return "API_KEY"
	default:
		return "" // idc/builderid: no tokentype header
	}
}
