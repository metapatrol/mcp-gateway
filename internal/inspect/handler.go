package inspect

import (
	"net/http"

	"github.com/go-logr/logr"

	"forgejo.metapatrol.local/platform/mcp-gateway/internal/inspect/api"
)

// BasePath is where the API is served, as the spec's servers entry says.
const BasePath = "/api/v1alpha1"

// Handler serves the inspect API. authenticate wraps every route but the
// API's own description and the RFC 9727 catalog pointing to it; it is
// required, because nothing else about this API is meant for whoever can
// reach the port.
func Handler(s *Server, authenticate func(http.Handler) http.Handler, log logr.Logger) http.Handler {
	badRequest := func(w http.ResponseWriter, _ *http.Request, err error) {
		writeProblem(w, http.StatusBadRequest, err.Error())
	}
	strict := api.NewStrictHandlerWithOptions(s, []api.StrictMiddlewareFunc{nameOperation}, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: badRequest,
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Error(err, "inspect request failed", "path", r.URL.Path)
			writeProblem(w, http.StatusInternalServerError, err.Error())
		},
	})
	h := api.HandlerWithOptions(strict, api.StdHTTPServerOptions{
		BaseURL:          BasePath,
		BaseRouter:       http.NewServeMux(),
		ErrorHandlerFunc: badRequest,
	})
	// The description and the catalog pointing to it are public; everything
	// else takes a token.
	mux := http.NewServeMux()
	for _, p := range publicPaths {
		mux.Handle("GET "+p, h)
	}
	mux.HandleFunc("GET "+APICatalogPath, func(w http.ResponseWriter, r *http.Request) {
		setOperation(r.Context(), "apiCatalog")
		apiCatalog(w, r)
	})
	mux.Handle("/", authenticate(h))
	return instrument(mux)
}
