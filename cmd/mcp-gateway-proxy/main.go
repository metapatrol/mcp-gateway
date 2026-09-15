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
	"strings"
	"syscall"
	"time"

	"forgejo.metapatrol.local/platform/mcp-gateway/internal/proxy"
)

var (
	version = "dev"
	commit  = "unknown"
	builtAt = "unknown"
)

// Every setting that is not a secret is a flag with an environment fallback,
// because the proxy runs in two places: as a sidecar, where a Deployment sets
// environment variables from a Secret written by the OAuth client's operator,
// and on a workstation, where flags are what people type.
func main() {
	var listenAddr string
	var gatewayURL string
	var tokenURL string
	var clientID string
	var clientSecretFile string
	var scopes string
	var audience string
	var allowedHosts string
	var logFormat string

	flag.StringVar(&listenAddr, "listen-address", env("MCP_GATEWAY_PROXY_LISTEN_ADDRESS", "127.0.0.1:8765"), "The address the local MCP endpoint binds to. Keep it on loopback: anything that reaches it acts with this proxy's grants.")
	flag.StringVar(&gatewayURL, "gateway-url", env("MCP_GATEWAY_URL", ""), "The gateway's MCP endpoint.")
	flag.StringVar(&tokenURL, "token-url", env("MCP_GATEWAY_TOKEN_URL", ""), "The OAuth token endpoint.")
	flag.StringVar(&clientID, "client-id", env("MCP_GATEWAY_CLIENT_ID", ""), "The OAuth client ID.")
	flag.StringVar(&clientSecretFile, "client-secret-file", env("MCP_GATEWAY_CLIENT_SECRET_FILE", ""), "File holding the OAuth client secret. MCP_GATEWAY_CLIENT_SECRET is read when unset.")
	flag.StringVar(&scopes, "scopes", env("MCP_GATEWAY_SCOPES", ""), "Comma-separated scopes to request.")
	flag.StringVar(&audience, "audience", env("MCP_GATEWAY_AUDIENCE", ""), "Audience to request, for providers that take an audience parameter.")
	flag.StringVar(&allowedHosts, "allowed-hosts", env("MCP_GATEWAY_PROXY_ALLOWED_HOSTS", ""), "Comma-separated Host header values accepted besides the loopback names.")
	flag.StringVar(&logFormat, "log-format", env("MCP_GATEWAY_PROXY_LOG_FORMAT", "text"), "Log format: text or json")
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
	log.Info("starting mcp-gateway-proxy", "version", version, "commit", commit, "builtAt", builtAt)

	clientSecret, err := readSecret(clientSecretFile, "MCP_GATEWAY_CLIENT_SECRET")
	if err != nil {
		log.Error("unable to read the client secret", "error", err)
		os.Exit(1)
	}

	var params url.Values
	if audience != "" {
		params = url.Values{"audience": {audience}}
	}
	p, err := proxy.New(proxy.Config{
		GatewayURL:     gatewayURL,
		TokenURL:       tokenURL,
		ClientID:       clientID,
		ClientSecret:   clientSecret,
		Scopes:         splitList(scopes),
		EndpointParams: params,
		AllowedHosts:   splitList(allowedHosts),
		HTTPClient:     &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()},
		Logger:         log,
	})
	if err != nil {
		log.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           p,
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

// readSecret reads a secret from a file, or from an environment variable when
// no file is named — never from a flag, which would put it in the process list.
func readSecret(file, env string) (string, error) {
	if file == "" {
		return os.Getenv(env), nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func splitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
