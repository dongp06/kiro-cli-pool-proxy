package proxy

import (
	"regexp"
	"strings"
)

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
