// Package proxy is mcp-gate-proxy: an auth-less local MCP endpoint that forwards
// to the gateway with a bearer token it obtains itself.
//
// It exists for agents that cannot, or should not, be configured with OAuth.
// The agent talks to localhost; the proxy runs the client credentials grant,
// keeps the token fresh, and passes every request through unchanged apart from
// the Authorization header.
//
// It is convenience and nothing more. It holds no policy and no backend
// credentials, and it is not a security boundary: whoever can reach its port
// acts with its client's grants. That is why it listens on loopback by default
// and refuses requests whose Host header or origin says a browser was steered
// at it.
//
// It forwards HTTP, not MCP. Sessions, streaming responses and notifications
// pass through as the gateway sends them, so the proxy needs no knowledge of the
// protocol and does not care which version the two ends speak.
package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// Config configures a Proxy.
type Config struct {
	// GatewayURL is the gateway's MCP endpoint.
	GatewayURL string

	// TokenURL, ClientID and ClientSecret are the OAuth client this proxy
	// authenticates as. The client's subject is what the gateway's grants
	// name.
	TokenURL     string
	ClientID     string
	ClientSecret string
	// Scopes are requested with the token. Optional.
	Scopes []string
	// EndpointParams are sent to the token endpoint in addition to the
	// standard ones — an audience, for providers that want one.
	EndpointParams url.Values

	// AllowedHosts are Host header values accepted besides the loopback
	// names. A proxy that serves a sidecar's pod IP, say, lists it here.
	AllowedHosts []string

	// MaxRequestBodyBytes bounds a request body. The body is held in memory so
	// that a request can be replayed with a fresh token. Defaults to 4 MiB,
	// the gateway's own limit.
	MaxRequestBodyBytes int64

	// HTTPClient carries requests to the token endpoint and the gateway. It
	// must not set a Timeout, which would cut the long-lived streams MCP
	// sessions keep open.
	HTTPClient *http.Client

	Logger *slog.Logger
}

// Proxy is the local endpoint.
type Proxy struct {
	cfg     Config
	gateway *url.URL
	tokens  *tokenCache
	handler http.Handler
}

// New builds a Proxy. It fetches nothing until the first request.
func New(cfg Config) (*Proxy, error) {
	gateway, err := url.Parse(cfg.GatewayURL)
	if err != nil || gateway.Scheme == "" || gateway.Host == "" {
		return nil, fmt.Errorf("proxy: the gateway URL %q must be absolute", cfg.GatewayURL)
	}
	if cfg.TokenURL == "" || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("proxy: the token URL, client ID and client secret are required")
	}
	if cfg.MaxRequestBodyBytes <= 0 {
		cfg.MaxRequestBodyBytes = 4 << 20
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}

	p := &Proxy{
		cfg:     cfg,
		gateway: gateway,
		tokens: &tokenCache{
			config: &clientcredentials.Config{
				ClientID:       cfg.ClientID,
				ClientSecret:   cfg.ClientSecret,
				TokenURL:       cfg.TokenURL,
				Scopes:         cfg.Scopes,
				EndpointParams: cfg.EndpointParams,
			},
			client: cfg.HTTPClient,
		},
	}

	forward := &httputil.ReverseProxy{
		Rewrite: p.rewrite,
		Transport: &bearerTransport{
			base:   cfg.HTTPClient.Transport,
			tokens: p.tokens,
			log:    cfg.Logger,
		},
		// Flush every write: responses are event streams, and buffering one
		// holds a tool result back until the buffer fills.
		FlushInterval:  -1,
		ModifyResponse: p.modifyResponse,
		ErrorHandler:   p.errorHandler,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("/", http.MaxBytesHandler(forward, cfg.MaxRequestBodyBytes))
	p.handler = p.guard(http.NewCrossOriginProtection().Handler(mux))
	return p, nil
}

// ServeHTTP implements http.Handler.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.handler.ServeHTTP(w, r)
}

// guard refuses requests addressed to a host name that is not this proxy's.
//
// This is the DNS rebinding defence. A web page cannot make a browser send a
// request to 127.0.0.1 under the page's own origin — unless the page's host name
// resolves to 127.0.0.1, and then the Host header still carries the page's name.
// Refusing every name but the loopback ones closes that door.
func (p *Proxy) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !p.hostAllowed(r.Host) {
			p.cfg.Logger.Warn("refused a request for a foreign host name", "host", r.Host, "remote", r.RemoteAddr)
			http.Error(w, "mcp-gate-proxy: host not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (p *Proxy) hostAllowed(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	return slices.Contains(p.cfg.AllowedHosts, host)
}

// rewrite addresses the request to the gateway. Every local path maps to the
// gateway's endpoint: the agent is configured with whatever URL the proxy
// listens on, and has no reason to know the gateway's.
func (p *Proxy) rewrite(pr *httputil.ProxyRequest) {
	pr.Out.URL = &url.URL{
		Scheme:   p.gateway.Scheme,
		Host:     p.gateway.Host,
		Path:     p.gateway.Path,
		RawQuery: pr.In.URL.RawQuery,
	}
	pr.Out.Host = p.gateway.Host
	// Whatever credentials the agent sent are not the proxy's to pass on.
	pr.Out.Header.Del("Authorization")
	pr.Out.Header.Del("Cookie")
}

// modifyResponse keeps OAuth out of the agent's view. A 401 that survived a
// token refresh means this proxy's own credentials are refused; passed through,
// its WWW-Authenticate challenge would send an OAuth-capable agent off to
// authorise itself, which is exactly what the proxy is there to spare it.
func (p *Proxy) modifyResponse(resp *http.Response) error {
	if resp.StatusCode == http.StatusUnauthorized {
		p.cfg.Logger.Error("the gateway refused this proxy's token", "clientID", p.cfg.ClientID)
		_ = resp.Body.Close()
		msg := "mcp-gate-proxy: the gateway refused this proxy's credentials\n"
		resp.StatusCode = http.StatusBadGateway
		resp.Status = http.StatusText(http.StatusBadGateway)
		resp.Header = http.Header{"Content-Type": {"text/plain; charset=utf-8"}}
		resp.Body = io.NopCloser(strings.NewReader(msg))
		resp.ContentLength = int64(len(msg))
	}
	resp.Header.Del("WWW-Authenticate")
	return nil
}

func (p *Proxy) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	var tokenErr *tokenError
	switch {
	case errors.As(err, &tokenErr):
		p.cfg.Logger.Error("obtaining an access token failed", "tokenURL", p.cfg.TokenURL, "error", tokenErr.err)
		http.Error(w, "mcp-gate-proxy: obtaining an access token failed", http.StatusBadGateway)
	case errors.Is(err, context.Canceled):
		// The agent went away; nobody is left to answer.
	default:
		p.cfg.Logger.Error("forwarding to the gateway failed", "method", r.Method, "error", err)
		http.Error(w, "mcp-gate-proxy: the gateway is unreachable", http.StatusBadGateway)
	}
}

// tokenError marks a failure to obtain a token, as opposed to a failure to
// reach the gateway.
type tokenError struct{ err error }

func (e *tokenError) Error() string { return "obtaining an access token: " + e.err.Error() }
func (e *tokenError) Unwrap() error { return e.err }

// tokenCache holds the current access token.
//
// It is refreshed shortly before it expires, and dropped when the gateway
// refuses it — a token can be revoked, or the gateway's clock can disagree,
// long before its exp says so.
type tokenCache struct {
	config *clientcredentials.Config
	client *http.Client

	mu    sync.Mutex
	token *oauth2.Token
}

// refreshMargin is how long before expiry a token is replaced. It covers the
// time a request spends on the way to the gateway plus modest clock skew.
const refreshMargin = 30 * time.Second

func (c *tokenCache) get(ctx context.Context) (*oauth2.Token, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != nil && (c.token.Expiry.IsZero() || time.Until(c.token.Expiry) > refreshMargin) {
		return c.token, nil
	}
	// Detached from the request: if this agent gives up, the next request is
	// waiting on the same lock for the same token.
	fetchCtx, cancel := context.WithTimeout(context.WithValue(context.WithoutCancel(ctx), oauth2.HTTPClient, c.client), 30*time.Second)
	defer cancel()
	token, err := c.config.Token(fetchCtx)
	if err != nil {
		return nil, &tokenError{err: err}
	}
	c.token = token
	return token, nil
}

// invalidate drops stale if it is still the current token. A token another
// request already replaced stays replaced.
func (c *tokenCache) invalidate(stale *oauth2.Token) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token == stale {
		c.token = nil
	}
}

// bearerTransport adds the token, and on a 401 retries once with a new one.
type bearerTransport struct {
	base   http.RoundTripper
	tokens *tokenCache
	log    *slog.Logger
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	// Buffer the body so the request can be sent twice. JSON-RPC messages are
	// small, and MaxBytesHandler bounds them before they get here.
	var body []byte
	if req.Body != nil && req.Body != http.NoBody {
		var err error
		body, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
	}

	token, err := t.tokens.get(req.Context())
	if err != nil {
		return nil, err
	}
	resp, err := base.RoundTrip(withToken(req, body, token))
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}

	t.log.Info("the gateway refused the token, fetching a new one")
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	t.tokens.invalidate(token)
	token, err = t.tokens.get(req.Context())
	if err != nil {
		return nil, err
	}
	return base.RoundTrip(withToken(req, body, token))
}

func withToken(req *http.Request, body []byte, token *oauth2.Token) *http.Request {
	out := req.Clone(req.Context())
	if body != nil {
		out.Body = io.NopCloser(bytes.NewReader(body))
		out.ContentLength = int64(len(body))
		out.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	}
	out.Header.Set("Authorization", "Bearer "+token.AccessToken)
	return out
}
