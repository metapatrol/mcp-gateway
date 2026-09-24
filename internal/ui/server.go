// Package ui is mcp-gateway-ui: it serves the console's web UI and passes the
// UI's API requests on to the gateway's inspect API.
//
// It holds no data and makes no decisions. The API the browser talks to is the
// gateway's own, at the same paths, so there is one API with one spec
// (api/openapi/inspect.yaml) and the UI's client is generated from it. What
// this server adds is the credential: the gateway wants a ServiceAccount
// token for its inspect audience, which a browser does not have and must not
// get. The server attaches its own and lets nothing else through — no method
// but GET and HEAD, no path outside the API, none of the browser's cookies or
// credentials.
//
// The console has no sign-in of its own. Who may open it is decided in front
// of it: a port-forward, or an ingress with an authenticating proxy.
package ui

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

// APIPrefix is where the inspect API is, on the gateway and here alike.
const APIPrefix = "/api/"

// Options configures the handler.
type Options struct {
	// UI is the built web UI; nil serves a page saying there is none.
	UI fs.FS
	// Gateway is the base URL of the gateway's inspect API Service, for
	// example http://mcp-gateway-inspect.agent-system.svc:8095.
	Gateway *url.URL
	// Token returns the bearer token to present to the gateway.
	Token func() (string, error)
	// Transport defaults to http.DefaultTransport.
	Transport http.RoundTripper
	Logger    *slog.Logger
}

// Handler serves the UI, the API proxy and a health endpoint.
func Handler(opts Options) http.Handler {
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	proxy := apiProxy(opts, log)
	mux.Handle(APIPrefix, proxy)
	// The API catalog, so a tool pointed at the console finds the API's
	// description as it would at the gateway.
	mux.Handle("/.well-known/api-catalog", proxy)
	mux.Handle("/", spa(opts.UI))
	return securityHeaders(mux)
}

func apiProxy(opts Options, log *slog.Logger) http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(opts.Gateway)
			// SetURL joins the base path with the request's; the API is at
			// the same path on the gateway as here.
			r.Out.Header.Del("Cookie")
			r.Out.Header.Del("Authorization")
			if token, ok := r.In.Context().Value(tokenKey{}).(string); ok {
				r.Out.Header.Set("Authorization", "Bearer "+token)
			}
		},
		Transport: opts.Transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Warn("the gateway did not answer", "path", r.URL.Path, "error", err)
			writeProblem(w, http.StatusBadGateway, "the gateway's inspect API did not answer")
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeProblem(w, http.StatusMethodNotAllowed, "the console is read-only")
			return
		}
		token, err := opts.Token()
		if err != nil {
			log.Error("no token for the gateway", "error", err)
			writeProblem(w, http.StatusServiceUnavailable, "the console has no token to present to the gateway")
			return
		}
		proxy.ServeHTTP(w, r.WithContext(withToken(r.Context(), token)))
	})
}

// spa serves the built UI. A path that names no file is a route of the
// application, and gets index.html; a missing asset — a path with an
// extension — is a plain 404 rather than a page the browser would try to run
// as a script.
func spa(ui fs.FS) http.Handler {
	if ui == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("This mcp-gateway-ui was built without its web UI (make ui-build). The inspect API is at " + APIPrefix + "v1alpha1/.\n"))
		})
	}
	files := http.FileServerFS(ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" && name != "index.html" {
			if info, err := fs.Stat(ui, name); err == nil && !info.IsDir() {
				// The build names every asset by its content hash.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				files.ServeHTTP(w, r)
				return
			}
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
		}
		index, err := fs.ReadFile(ui, "index.html")
		if err != nil {
			http.Error(w, "index.html is missing from the UI build", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// Angular sets style attributes at runtime, hence 'unsafe-inline' for
		// styles; scripts come from this origin only.
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"title": http.StatusText(status), "status": status, "detail": detail})
}

// FileToken reads a projected ServiceAccount token. The kubelet rewrites the
// file before the token expires, so it is read again once it is a minute old
// rather than once at startup.
func FileToken(file string) func() (string, error) {
	var mu sync.Mutex
	var token string
	var read time.Time
	return func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if token != "" && time.Since(read) < time.Minute {
			return token, nil
		}
		b, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		t := strings.TrimSpace(string(b))
		if t == "" {
			return "", errors.New(file + " is empty")
		}
		token, read = t, time.Now()
		return token, nil
	}
}
