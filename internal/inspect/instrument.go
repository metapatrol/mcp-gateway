package inspect

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"forgejo.metapatrol.local/platform/mcp-gateway/internal/inspect/api"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/metrics"
)

// Requests are counted by the spec's operationId rather than by path: a path
// carries a resource name, and a label made of it would mint a series per
// resource. The operation is only known once the generated router has chosen
// one, deeper in the chain than the status is, so the outermost handler puts
// a slot in the context and the strict middleware fills it.

type operationKey struct{}

// noOperation labels a request that reached none: an unknown path, or one
// refused before routing.
const noOperation = "none"

// streamOperation is left out of the duration histogram.
const streamOperation = "watchEvents"

func instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		op := noOperation
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), operationKey{}, &op)))
		metrics.InspectRequests.WithLabelValues(op, strconv.Itoa(rec.status)).Inc()
		if op != streamOperation {
			metrics.InspectRequestDuration.WithLabelValues(op).Observe(time.Since(start).Seconds())
		}
	})
}

// nameOperation is the strict middleware that tells instrument which
// operation a request is. The generator hands it the Go name, GetServer for
// the spec's getServer; the label is the spec's.
func nameOperation(f api.StrictHandlerFunc, goName string) api.StrictHandlerFunc {
	operationID := strings.ToLower(goName[:1]) + goName[1:]
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		setOperation(ctx, operationID)
		return f(ctx, w, r, request)
	}
}

func setOperation(ctx context.Context, operationID string) {
	if op, ok := ctx.Value(operationKey{}).(*string); ok {
		*op = operationID
	}
}

// statusRecorder remembers the status written. It flushes, because the event
// stream does, and unwraps, for http.ResponseController.
type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.written {
		s.status, s.written = code, true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.written = true
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Flush() {
	s.written = true
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }
