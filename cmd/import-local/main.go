// Command import-local reads credentials from a local kiro-cli SQLite database
// and prints an account JSON block ready to paste into config.json.
package main

import (
	"encoding/json"
	"fmt"
	"kiro-cli-pool-proxy/kirolocal"
	"os"
)

func main() {
	path := ""
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	acc, err := kirolocal.ImportAccount(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Imported %s account (region=%s)\n", acc.AuthMethod, acc.Region)
	if acc.ProfileArn == "" {
		fmt.Fprintf(os.Stderr, "⚠️  profileArn empty — add it manually for chat to work.\n")
	}

	out, _ := json.MarshalIndent(acc, "", "  ")
	fmt.Println("// Paste this into the \"accounts\" array of config.json:")
	fmt.Println(string(out))
}
