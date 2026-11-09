package gateway

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"forgejo.metapatrol.local/platform/mcp-gateway/internal/auth"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/metrics"
)

// HandlerOptions configures the HTTP side of the gateway.
type HandlerOptions struct {
	// Path is where the MCP endpoint is served. Defaults to /mcp.
	Path string

	// Verifier checks bearer tokens.
	Verifier sdkauth.TokenVerifier

	// ResourceURL is the gateway's public URL for its MCP endpoint, as agents
	// reach it — for example https://mcp-gateway.example.com/mcp. With
	// AuthorizationServers it enables RFC 9728 protected resource metadata:
	// a 401 then tells an agent where to get a token, instead of just saying
	// no.
	ResourceURL string

	// AuthorizationServers are the issuer URLs published in the protected
	// resource metadata.
	AuthorizationServers []string

	// ScopesSupported are published in the metadata. They are informational:
	// the gateway authorises by MCPGrant, not by scope.
	ScopesSupported []string

	// Logger receives the SDK's transport logging. Nil discards it.
	Logger *slog.Logger
}

// Handler returns the gateway's HTTP handler: the authenticated MCP endpoint
// and, when configured, its protected resource metadata.
func (g *Gateway) Handler(opts HandlerOptions) (http.Handler, error) {
	if opts.Verifier == nil {
		return nil, errors.New("gateway: a token verifier is required")
	}
	if opts.Path == "" {
		opts.Path = "/mcp"
	}

	mux := http.NewServeMux()

	var metadataURL string
	if opts.ResourceURL != "" && len(opts.AuthorizationServers) > 0 {
		var err error
		metadataURL, err = protectedResourceMetadataURL(opts.ResourceURL)
		if err != nil {
			return nil, err
		}
		metadata := sdkauth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
			Resource:               opts.ResourceURL,
			AuthorizationServers:   opts.AuthorizationServers,
			ScopesSupported:        opts.ScopesSupported,
			BearerMethodsSupported: []string{"header"},
			ResourceName:           "mcp-gateway",
		})
		u, _ := url.Parse(metadataURL)
		mux.Handle(u.Path, metadata)
		// Clients that predate RFC 9728's path-aware form ask at the root. A
		// resource URL without a path — the endpoint served at / — already
		// has its metadata there, and a second pattern for it would panic.
		if root := "/.well-known/oauth-protected-resource"; u.Path != root {
			mux.Handle(root, metadata)
		}
	}

	streamable := mcp.NewStreamableHTTPHandler(g.serverForRequest, streamableOptions(opts.Logger))
	requireToken := sdkauth.RequireBearerToken(opts.Verifier, &sdkauth.RequireBearerTokenOptions{
		ResourceMetadataURL: metadataURL,
		// An introspection answer may carry no exp; the provider's "active"
		// is then the whole verdict. JWTs without exp are refused by the
		// verifier itself.
		AllowMissingExpiration: true,
		ClockSkew:              30 * time.Second,
	})
	mux.Handle(opts.Path, countMissingTokens(requireToken(requireProtocol(streamable))))
	return mux, nil
}

// serverForRequest picks the subject's server. The token has been verified by
// the time this runs; a request without a subject gets no server, which the
// SDK answers with 400.
func (g *Gateway) serverForRequest(r *http.Request) *mcp.Server {
	info := sdkauth.TokenInfoFromContext(r.Context())
	if info == nil || info.UserID == "" {
		return nil
	}
	return g.serverFor(info.UserID)
}

// countMissingTokens records requests that arrive without any bearer token. The
// verifier counts the tokens it refuses, and never sees these.
func countMissingTokens(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fields := strings.Fields(r.Header.Get("Authorization")); len(fields) != 2 || !strings.EqualFold(fields[0], "bearer") {
			metrics.RecordAuthFailure(auth.ReasonMissingToken)
		}
		next.ServeHTTP(w, r)
	})
}

// requireProtocol refuses every request that does not name MCP 2026-07-28 in
// its MCP-Protocol-Version header. The SDK takes a request without the header
// for one of the session-based protocols and answers it — initialize included,
// with a version the next request is refused for. A client of those protocols
// would get halfway and then fail; refused here, it learns at once what the
// gateway speaks.
func requireProtocol(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch v := r.Header.Get("MCP-Protocol-Version"); v {
		case ProtocolVersion:
			next.ServeHTTP(w, r)
		case "":
			http.Error(w, "Bad Request: missing MCP-Protocol-Version header (supported versions: "+ProtocolVersion+")", http.StatusBadRequest)
		default:
			http.Error(w, "Bad Request: Unsupported protocol version (supported versions: "+ProtocolVersion+")", http.StatusBadRequest)
		}
	})
}

// protectedResourceMetadataURL derives the RFC 9728 metadata location for a
// resource: the well-known segment goes between the host and the path, so
// https://gw.example.com/mcp is described at
// https://gw.example.com/.well-known/oauth-protected-resource/mcp.
func protectedResourceMetadataURL(resource string) (string, error) {
	u, err := url.Parse(resource)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("gateway: the resource URL must be absolute")
	}
	u.Path = "/.well-known/oauth-protected-resource" + strings.TrimSuffix(u.Path, "/")
	u.RawQuery, u.Fragment = "", ""
	return u.String(), nil
}

// streamableOptions configures the Streamable HTTP handler agents reach — and
// the one a preview is answered through.
func streamableOptions(logger *slog.Logger) *mcp.StreamableHTTPOptions {
	return &mcp.StreamableHTTPOptions{
		// MCP 2026-07-28 has no sessions: every request stands on its own, and
		// a client that wants to hear about changes holds a subscriptions/listen
		// stream open. Any replica answers any request, and every replica
		// keeps each subject's tools in step with the API on its own, so a
		// listen stream on one replica learns what a grant change on any of
		// them means. The SDK serves that protocol only on a stateless handler.
		Stateless: true,
		Logger:    logger,
		// The SDK refuses requests that arrive over loopback with a foreign
		// Host header, against DNS rebinding. That defends an endpoint a
		// browser could use unauthenticated, which this one is not — every
		// request needs a bearer token no web page has. It would, however,
		// refuse all traffic behind a service mesh that delivers inbound
		// requests to the pod over loopback.
		DisableLocalhostProtection: true,
	}
}

// HTTPServer runs an http.Server as a manager.Runnable.
type HTTPServer struct {
	// Name says what is served, in the log line at startup. Defaults to MCP.
	Name    string
	Addr    string
	Handler http.Handler
	// ShutdownTimeout bounds the graceful shutdown. Open SSE streams do not
	// end on their own, so without a bound shutdown would wait for agents to
	// hang up. Defaults to 10s.
	ShutdownTimeout time.Duration
	Log             func(msg string, keysAndValues ...any)
}

// Start serves until ctx ends.
func (s *HTTPServer) Start(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.Addr,
		Handler:           s.Handler,
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: a tool call streams its answer for as long as the
		// tool runs, and a session's GET stream lives as long as the session.
	}
	timeout := s.ShutdownTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	errc := make(chan error, 1)
	go func() {
		if s.Log != nil {
			name := s.Name
			if name == "" {
				name = "MCP"
			}
			s.Log("serving "+name, "address", s.Addr)
		}
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			_ = srv.Close()
		}
		return nil
	}
}

// NeedLeaderElection is false: every replica serves traffic.
func (s *HTTPServer) NeedLeaderElection() bool { return false }
