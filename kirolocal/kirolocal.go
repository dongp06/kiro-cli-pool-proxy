// Package kirolocal reads Kiro CLI credentials from its local SQLite database.
// Schema reverse-engineered from kiro-cli 2.12.2 (see REVERSE_ENGINEERING.md):
//
//	auth_kv["kirocli:odic:token"]                -> access/refresh token, expiry, region
//	auth_kv["kirocli:odic:device-registration"]  -> client_id, client_secret
//	auth_kv["kirocli:social:token"]              -> social token
//	auth_kv["kirocli:external-idp:token"]        -> external IdP token
package kirolocal

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"kiro-cli-pool-proxy/config"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// DefaultDBPath returns the default kiro-cli SQLite path.
func DefaultDBPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "kiro-cli", "data.sqlite3")
}

// ReadAuthKV extracts all auth_kv rows via a pure-Go SQLite driver.
func ReadAuthKV(dbPath string) (map[string]string, error) {
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

// ImportAccount reads the local kiro-cli DB and builds a config.Account.
// If dbPath is empty, the default path is used.
func ImportAccount(dbPath string) (*config.Account, error) {
	if dbPath == "" {
		dbPath = DefaultDBPath()
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("database not found: %s", dbPath)
	}
	kv, err := ReadAuthKV(dbPath)
	if err != nil {
		return nil, err
	}
	return buildAccount(kv)
}

func buildAccount(kv map[string]string) (*config.Account, error) {
	tokenRaw, ok := kv["kirocli:odic:token"]
	authMethod := "idc"
	if !ok {
		if s, ok2 := kv["kirocli:social:token"]; ok2 {
			tokenRaw, authMethod = s, "social"
		} else if e, ok3 := kv["kirocli:external-idp:token"]; ok3 {
			tokenRaw, authMethod = e, "external_idp"
		} else {
			return nil, fmt.Errorf("no known token key found in auth_kv")
		}
	}

	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresAt    string `json:"expires_at"`
		Region       string `json:"region"`
	}
	if err := json.Unmarshal([]byte(tokenRaw), &tok); err != nil {
		return nil, fmt.Errorf("parse token json: %w", err)
	}

	acc := &config.Account{
		ID:           "imported-" + time.Now().Format("20060102-150405"),
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		AuthMethod:   authMethod,
		Region:       tok.Region,
		Enabled:      true,
	}
	if tok.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, tok.ExpiresAt); err == nil {
			acc.ExpiresAt = t.Unix()
		}
	}
	if acc.Region == "" {
		acc.Region = "us-east-1"
	}

	if regRaw, ok := kv["kirocli:odic:device-registration"]; ok {
		var reg struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		}
		if json.Unmarshal([]byte(regRaw), &reg) == nil {
			acc.ClientID = reg.ClientID
			acc.ClientSecret = reg.ClientSecret
		}
	}
	return acc, nil
}
