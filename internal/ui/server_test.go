package ui

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gateway records what reaches it.
type gateway struct {
	*httptest.Server
	last *http.Request
}

func newGateway(t *testing.T) *gateway {
	g := &gateway{}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.last = r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[]}`)
	}))
	t.Cleanup(g.Close)
	return g
}

func handler(t *testing.T, g *gateway, ui fstest.MapFS, token func() (string, error)) http.Handler {
	t.Helper()
	u, err := url.Parse(g.URL)
	require.NoError(t, err)
	opts := Options{Gateway: u, Token: token}
	if ui != nil {
		opts.UI = ui
	}
	return Handler(opts)
}

func serve(h http.Handler, method, target string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func static(token string) func() (string, error) {
	return func() (string, error) { return token, nil }
}

var built = fstest.MapFS{
	"index.html":        {Data: []byte("<app-root></app-root>")},
	"main-ABCD1234.js":  {Data: []byte("console.log(1)")},
	"media/logo.svg":    {Data: []byte("<svg/>")},
	"styles-EF567.css":  {Data: []byte("body{}")},
	"favicon.ico":       {Data: []byte{0}},
	"nested/index.html": {Data: []byte("nested")},
}

func TestAPIProxy(t *testing.T) {
	g := newGateway(t)
	h := handler(t, g, built, static("sa-token"))

	rec := serve(h, http.MethodGet, "/api/v1alpha1/subject?subject=https%3A%2F%2Fidp%2Fx", http.Header{
		"Authorization": {"Bearer from-the-browser"},
		"Cookie":        {"session=abc"},
	})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"items":[]}`, rec.Body.String())
	require.NotNil(t, g.last)
	assert.Equal(t, "/api/v1alpha1/subject", g.last.URL.Path)
	assert.Equal(t, "https://idp/x", g.last.URL.Query().Get("subject"), "the query arrives intact")
	assert.Equal(t, "Bearer sa-token", g.last.Header.Get("Authorization"), "the console's own token, never the browser's")
	assert.Empty(t, g.last.Header.Get("Cookie"))

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		g.last = nil
		rec := serve(h, method, "/api/v1alpha1/servers", nil)
		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, method)
		assert.Nil(t, g.last, "%s never reaches the gateway", method)
	}
}

func TestAPIProxyWithoutToken(t *testing.T) {
	g := newGateway(t)
	h := handler(t, g, built, func() (string, error) { return "", errors.New("no file") })
	rec := serve(h, http.MethodGet, "/api/v1alpha1/servers", nil)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	assert.Nil(t, g.last)
}

func TestAPIProxyGatewayDown(t *testing.T) {
	g := newGateway(t)
	h := handler(t, g, built, static("t"))
	g.Close()
	rec := serve(h, http.MethodGet, "/api/v1alpha1/servers", nil)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestSPA(t *testing.T) {
	g := newGateway(t)
	h := handler(t, g, built, static("t"))

	for _, route := range []string{"/", "/toolsets/si-analysis", "/subjects", "/index.html"} {
		rec := serve(h, http.MethodGet, route, nil)
		assert.Equal(t, http.StatusOK, rec.Code, route)
		assert.Equal(t, "<app-root></app-root>", rec.Body.String(), route)
		assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"), route)
	}

	rec := serve(h, http.MethodGet, "/main-ABCD1234.js", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Cache-Control"), "immutable")

	assert.Equal(t, http.StatusNotFound, serve(h, http.MethodGet, "/main-GONE.js", nil).Code, "a missing asset is not the app")
	assert.Equal(t, http.StatusMethodNotAllowed, serve(h, http.MethodPost, "/", nil).Code)

	rec = serve(h, http.MethodGet, "/", nil)
	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "default-src 'self'")
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
}

func TestWithoutUI(t *testing.T) {
	g := newGateway(t)
	h := handler(t, g, nil, static("t"))
	rec := serve(h, http.MethodGet, "/", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "make ui-build")
	assert.Equal(t, http.StatusOK, serve(h, http.MethodGet, "/api/v1alpha1/servers", nil).Code, "the API still works")
}

func TestHealthz(t *testing.T) {
	g := newGateway(t)
	assert.Equal(t, http.StatusNoContent, serve(handler(t, g, nil, static("t")), http.MethodGet, "/healthz", nil).Code)
}

func TestFileToken(t *testing.T) {
	file := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(file, []byte("first\n"), 0o600))
	token := FileToken(file)
	got, err := token()
	require.NoError(t, err)
	assert.Equal(t, "first", got)

	require.NoError(t, os.WriteFile(file, []byte("second"), 0o600))
	got, _ = token()
	assert.Equal(t, "first", got, "read again only once it is a minute old")

	_, err = FileToken(filepath.Join(t.TempDir(), "missing"))()
	assert.Error(t, err)
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	_, err = FileToken(file)()
	assert.Error(t, err)
}
