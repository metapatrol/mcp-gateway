package inspect

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"forgejo.metapatrol.local/platform/mcp-gateway/api/openapi"
)

// refuseAll stands for an authenticator no request passes.
func refuseAll(http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		unauthorized(w, "no token")
	})
}

func TestDescriptionIsPublic(t *testing.T) {
	h := Handler(&Server{}, refuseAll, logr.Discard())
	get := func(target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec
	}

	rec := get(BasePath + "/openapi.yaml")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/yaml", rec.Header().Get("Content-Type"))
	assert.Equal(t, string(openapi.InspectYAML), rec.Body.String(), "the file the code was generated from, as written")

	rec = get(BasePath + "/openapi.json")
	require.Equal(t, http.StatusOK, rec.Code)
	var doc struct {
		OpenAPI string         `json:"openapi"`
		Paths   map[string]any `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
	assert.Equal(t, "3.0.3", doc.OpenAPI)
	assert.Contains(t, doc.Paths, "/servers")

	rec = get(APICatalogPath)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/linkset+json")
	var catalog struct {
		Linkset []struct {
			Anchor      string `json:"anchor"`
			ServiceDesc []struct {
				Href string `json:"href"`
				Type string `json:"type"`
			} `json:"service-desc"`
		} `json:"linkset"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &catalog))
	require.Len(t, catalog.Linkset, 1)
	assert.Equal(t, BasePath, catalog.Linkset[0].Anchor)
	require.Len(t, catalog.Linkset[0].ServiceDesc, 2)
	for _, d := range catalog.Linkset[0].ServiceDesc {
		assert.Equal(t, http.StatusOK, get(d.Href).Code, "the catalog points at %s, and it is there", d.Href)
	}

	// Everything else still takes a token.
	for _, target := range []string{BasePath + "/servers", BasePath + "/events", BasePath + "/openapi.yml", "/.well-known/other"} {
		assert.Equal(t, http.StatusUnauthorized, get(target).Code, target)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, BasePath+"/openapi.yaml", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "only reading the description is public")
}
