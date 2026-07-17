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
		log.Fatalf("No enabled accounts in config. Add accounts to %s", *configPath)
	}

	auth.StartBackgroundRefresh(cfg)
	auth.StartQuotaPoller(cfg, 5*time.Minute)

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

	log.Printf("╔══════════════════════════════════════════════════════════════╗")
	log.Printf("║   Kiro CLI Pool Proxy  (plain reverse-proxy mode)            ║")
	log.Printf("╠══════════════════════════════════════════════════════════════╣")
	log.Printf("║   Listen:   %-48s ║", cfg.ListenAddr)
	log.Printf("║   Accounts: %-48d ║", enabledCount)
	log.Printf("║   Strategy: %-48s ║", cfg.Strategy)
	log.Printf("╠══════════════════════════════════════════════════════════════╣")
	log.Printf("║   Point kiro-cli at this proxy (no MITM/cert needed):        ║")
	log.Printf("║   kiro-cli settings api.krs.service \\                        ║")
	log.Printf("║     '{\"endpoint\":\"%s\",\"region\":\"us-east-1\"}'  ", proxyURL)
	log.Printf("║   kiro-cli settings api.cps.service \\                        ║")
	log.Printf("║     '{\"endpoint\":\"%s\",\"region\":\"us-east-1\"}'  ", proxyURL)
	log.Printf("║   (or run ./set-endpoints.sh %s )      ", proxyURL)
	log.Printf("╠══════════════════════════════════════════════════════════════╣")
	log.Printf("║   Admin panel: %s/admin", proxyURL)
	log.Printf("╚══════════════════════════════════════════════════════════════╝")

	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

func createTemplateConfig(path string) {
	template := `{
  "listenAddr": "0.0.0.0:9999",
  "strategy": "smart",
  "poolKey": "",
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
