package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	maxbotlib "github.com/max-messenger/max-bot-api-client-go"

	"first-max-bot/bot"
	"first-max-bot/config"
	"first-max-bot/db"
	"first-max-bot/services/maxbot"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	database, err := db.New(cfg.DBDriver, cfg.DBPath, cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	if err := database.Migrate(); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	client, err := maxbot.NewClient(maxbotlib.WithHTTPClient(buildHTTPClient(cfg)))
	if err != nil {
		log.Fatalf("maxbot: %v", err)
	}

	info, err := client.GetBot(ctx)
	if err != nil {
		log.Printf("GetBot: %v", err)
	} else {
		log.Printf("Bot started: %s (id=%d)", info.Name, info.UserId)
	}

	b := bot.New(client, database, cfg)
	b.Run(ctx)
}

// buildHTTPClient returns an http.Client tuned for the Max API.
// The Russian Trusted CA is not in the macOS default pool, so we allow
// either a custom CA file (CA_CERT_FILE) or dev-only skip verify.
func buildHTTPClient(cfg *config.Config) *http.Client {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}

	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			log.Printf("read CA file %s: %v", cfg.CAFile, err)
		} else if pool, ok := newCertPool(pem); ok {
			tlsConfig.RootCAs = pool
		}
	}

	if cfg.InsecureSkipVerify {
		tlsConfig.InsecureSkipVerify = true
	}

	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
	}
}

func newCertPool(pem []byte) (*x509.CertPool, bool) {
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(pem); !ok {
		log.Printf("no valid certificates in CA file")
		return nil, false
	}
	return pool, true
}
