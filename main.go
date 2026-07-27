package main

import (
	"flag"
	"fmt"
	"kiro-cli-pool-proxy/auth"
	"kiro-cli-pool-proxy/config"
	"kiro-cli-pool-proxy/pool"
	"kiro-cli-pool-proxy/proxy"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	configPath := flag.String("config", "config.json", "Path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		if os.IsNotExist(err) {
			createTemplateConfig(*configPath)
			fmt.Printf("Created template config at %s — edit it with your accounts, then re-run.\n", *configPath)
			os.Exit(0)
		}
		log.Fatalf("Failed to load config: %v", err)
	}

	p := pool.New(cfg)
	enabledCount := p.AvailableCount()
	if enabledCount == 0 {
		log.Printf("No enabled accounts yet — add them via the admin panel or edit %s. Proxy will start anyway.", *configPath)
	}

	auth.StartBackgroundRefresh(cfg)

	// Seed the model-resolution overlay from any previously-synced model list.
	if models := cfg.GetKiroModels(); len(models) > 0 {
		proxy.SetSyncedKiroModels(models)
	}

	// Persist usage counters periodically.
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			cfg.Save()
		}
	}()

	server := proxy.NewServer(cfg, p)
	httpServer := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: server,
		// No write timeout: chat streams run for minutes.
		ReadHeaderTimeout: 30 * time.Second,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		httpServer.Close()
		cfg.Save()
	}()

	host, port, _ := net.SplitHostPort(cfg.ListenAddr)
	displayHost := host
	if host == "" || host == "0.0.0.0" || host == "::" {
		displayHost = "<SERVER_IP>"
	}
	proxyURL := fmt.Sprintf("http://%s:%s", displayHost, port)

	log.Printf("KiroPool listening on %s · %d accounts · strategy=%s · admin %s/admin", cfg.ListenAddr, enabledCount, cfg.Strategy, proxyURL)

	if cfg.GetAdminPassword() == "" {
		// Never leave the panel unprotected: seed the default password,
		// persist it to config, and warn so the operator changes it.
		if cfg.SetAdminPassword(config.DefaultAdminPassword) {
			if err := cfg.Save(); err != nil {
				log.Printf("WARNING: set the default admin password but failed to save it to %s: %v — set one in the panel or export %s.", *configPath, err, config.AdminPasswordEnvKey)
			} else {
				log.Printf("No admin password was set; seeded the default %q into %s.", config.DefaultAdminPassword, *configPath)
				log.Printf("  >>> Log in at %s/admin and CHANGE it from Settings before exposing the panel. Or export %s.", proxyURL, config.AdminPasswordEnvKey)
			}
		} else {
			log.Printf("Admin password is pinned by %s (not editable from the panel).", config.AdminPasswordEnvKey)
		}
	} else if config.AdminPasswordFromEnv() {
		log.Printf("Admin password sourced from %s (not editable from the panel).", config.AdminPasswordEnvKey)
	} else if cfg.GetAdminPassword() == config.DefaultAdminPassword {
		log.Printf("WARNING: admin panel is using the default password %q — change it from Settings before exposing the panel.", config.DefaultAdminPassword)
	}

	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

func createTemplateConfig(path string) {
	template := `{
  "listenAddr": "0.0.0.0:5000",
  "strategy": "smart",
  "poolKey": "",
  "adminPassword": "changeme",
  "accounts": [
    {
      "id": "account-1",
      "email": "user1@example.com",
      "accessToken": "YOUR_ACCESS_TOKEN",
      "refreshToken": "YOUR_REFRESH_TOKEN",
      "clientId": "YOUR_CLIENT_ID",
      "clientSecret": "YOUR_CLIENT_SECRET",
      "authMethod": "idc",
      "region": "us-east-1",
      "profileArn": "arn:aws:codewhisperer:us-east-1:123456789:profile/xxxxxxxx",
      "expiresAt": 0,
      "enabled": true
    }
  ]
}
`
	os.WriteFile(path, []byte(template), 0600)
}
