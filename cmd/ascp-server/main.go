// Command ascp-server starts the runnable ASCP reference service. The service
// exposes both a free one-call read capability and a contracted send capability
// with files and multiple billing arrangements.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/LuoShenKui/agent-service-contract-protocol/internal/email"
	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/ascp"
	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/audit"
	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/billing"
	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	address := environment("ASCP_ADDR", ":8080")
	baseURL := strings.TrimRight(environment("ASCP_BASE_URL", "http://localhost:8080"), "/")
	token := environment("ASCP_DEMO_TOKEN", "ascp-demo-token")

	signer, ephemeral, err := loadSigner()
	if err != nil {
		logger.Error("could not configure ASCP signing key", "error", err)
		os.Exit(1)
	}
	if ephemeral {
		logger.Warn("using an ephemeral signing key; set ASCP_SIGNING_PRIVATE_KEY_B64 for persistent deployments")
	}

	identity := server.Identity{
		Actor:     ascp.EntityRef{Type: "agent", ID: "demo-client"},
		Principal: ascp.EntityRef{Type: "user", ID: "demo-user"},
		Scopes:    map[string]bool{"email.read": true, "email.send": true},
	}

	engine, err := server.New(server.Config{
		Manifest: ascp.Manifest{
			ServiceID:        "urn:ascp:service:reference-email",
			ServiceName:      "ASCP reference email service",
			BaseURL:          baseURL,
			JWKSURI:          baseURL + "/.well-known/jwks.json",
			CapabilitiesURI:  baseURL + "/.well-known/ascp/capabilities",
			OptionsURI:       baseURL + "/v1/options",
			InvokeURI:        baseURL + "/v1/invoke",
			FilesURI:         baseURL + "/v1/files",
			DocumentationURI: "https://github.com/LuoShenKui/agent-service-contract-protocol",
			AuthSchemes:      []string{"bearer-demo"},
			BillingModes: []ascp.BillingMode{
				ascp.BillingFree,
				ascp.BillingPayNow,
				ascp.BillingPrepaidBalance,
				ascp.BillingSubscription,
				ascp.BillingPostpaid,
				ascp.BillingMonthlyInvoice,
				ascp.BillingClearing,
				ascp.BillingSponsored,
				ascp.BillingExternal,
			},
			PaymentSchemes: []string{"urn:ascp:billing:mock-pay-now"},
			Limits: map[string]int64{
				"maximum_body_bytes":        1 << 20,
				"idempotency_retention_sec": 86400,
			},
		},
		Authenticator: server.StaticAuthenticator{Token: token, Identity: identity},
		Authorization: server.DemoAuthorizationVerifier{},
		Service:       email.NewService(),
		Store:         server.NewMemoryStore(),
		Idempotency:   server.NewIdempotencyStore(),
		Audit:         audit.NewLog(signer),
		Billing:       billing.NewMockProcessor(),
		Files:         server.NewMemoryFileStore(10 << 20),
		Signer:        signer,
		Logger:        logger,
	})
	if err != nil {
		logger.Error("could not create ASCP server", "error", err)
		os.Exit(1)
	}

	httpServer := &http.Server{
		Addr:              address,
		Handler:           engine,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}

	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-shutdownSignals
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
		}
	}()

	logger.Info("ASCP reference service listening",
		"address", address,
		"base_url", baseURL,
		"actor", identity.Actor.ID,
		"principal", identity.Principal.ID,
	)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("ASCP server stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}

// loadSigner accepts a base64-encoded raw Ed25519 private key. Production
// operators should normally inject this value from a secret manager or replace
// the signer with an HSM/KMS-backed implementation.
func loadSigner() (*ascp.Signer, bool, error) {
	encoded := strings.TrimSpace(os.Getenv("ASCP_SIGNING_PRIVATE_KEY_B64"))
	if encoded == "" {
		signer, err := ascp.NewSigner("reference-ephemeral-1")
		return signer, true, err
	}

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil {
		return nil, false, err
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, false, errors.New("ASCP_SIGNING_PRIVATE_KEY_B64 must decode to a 64-byte Ed25519 private key")
	}
	signer, err := ascp.NewSignerFromPrivateKey(environment("ASCP_SIGNING_KEY_ID", "reference-1"), ed25519.PrivateKey(raw))
	return signer, false, err
}

func environment(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
