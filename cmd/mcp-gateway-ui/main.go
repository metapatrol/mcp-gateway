package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"forgejo.metapatrol.local/platform/mcp-gateway/internal/ui"
)

var (
	version = "dev"
	commit  = "unknown"
	builtAt = "unknown"
)

func main() {
	var listenAddr string
	var gatewayURL string
	var tokenFile string
	var logFormat string

	flag.StringVar(&listenAddr, "listen-address", env("MCP_GATEWAY_UI_LISTEN_ADDRESS", ":8080"), "The address the console binds to.")
	flag.StringVar(&gatewayURL, "gateway-url", env("MCP_GATEWAY_UI_GATEWAY_URL", ""), "Base URL of the gateway's inspect API, e.g. http://mcp-gateway-inspect.agent-system.svc:8095.")
	flag.StringVar(&tokenFile, "token-file", env("MCP_GATEWAY_UI_TOKEN_FILE", "/var/run/secrets/mcp-gateway/inspect/token"), "ServiceAccount token, projected for the inspect API's audience, presented to the gateway.")
	flag.StringVar(&logFormat, "log-format", env("MCP_GATEWAY_UI_LOG_FORMAT", "json"), "Log format: text or json")
	flag.Parse()

	var handler slog.Handler
	switch logFormat {
	case "text":
		handler = slog.NewTextHandler(os.Stderr, nil)
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, nil)
	default:
		fmt.Fprintf(os.Stderr, "invalid --log-format %q: must be text or json\n", logFormat)
		os.Exit(1)
	}
	log := slog.New(handler)
	log.Info("starting mcp-gateway-ui", "version", version, "commit", commit, "builtAt", builtAt)

	gateway, err := url.Parse(gatewayURL)
	if err != nil || gateway.Scheme == "" || gateway.Host == "" {
		log.Error("--gateway-url must be an absolute URL", "gatewayURL", gatewayURL)
		os.Exit(1)
	}
	built := ui.Built()
	if built == nil {
		log.Warn("built without the web UI: serving the API proxy only")
	}

	srv := &http.Server{
		Addr: listenAddr,
		Handler: ui.Handler(ui.Options{
			UI:      built,
			Gateway: gateway,
			Token:   ui.FileToken(tokenFile),
			Logger:  log,
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			_ = srv.Close()
		}
	}()

	log.Info("serving", "address", listenAddr, "gateway", gatewayURL)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func env(name, fallback string) string {
	if v, ok := os.LookupEnv(name); ok {
		return v
	}
	return fallback
}
