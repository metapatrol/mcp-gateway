package inspect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"forgejo.metapatrol.local/platform/mcp-gateway/internal/metrics"
)

func TestRequestsAreCountedByOperation(t *testing.T) {
	h := newTestServer(t)
	count := func(op, code string) float64 {
		return testutil.ToFloat64(metrics.InspectRequests.WithLabelValues(op, code))
	}
	serve := func(target string) {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil))
	}

	before := count("getServer", "200")
	serve(BasePath + "/servers/search")
	assert.Equal(t, before+1, count("getServer", "200"), "by operationId, not by path")

	before = count("getServer", "404")
	serve(BasePath + "/servers/nope")
	assert.Equal(t, before+1, count("getServer", "404"))

	before = count(noOperation, "404")
	serve(BasePath + "/nothing-here")
	assert.Equal(t, before+1, count(noOperation, "404"))

	before = count("apiCatalog", "200")
	serve(APICatalogPath)
	assert.Equal(t, before+1, count("apiCatalog", "200"))

	refused := Handler(&Server{}, refuseAll, logr.Discard())
	before = count(noOperation, "401")
	refused.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, BasePath+"/servers", nil))
	assert.Equal(t, before+1, count(noOperation, "401"), "refused before any operation was chosen")
}

func TestEventStreamsAreCounted(t *testing.T) {
	e := &Events{Window: 20 * time.Millisecond, Heartbeat: time.Hour}
	startEvents(t, e)
	srv := httptest.NewServer(Handler(&Server{Events: e}, func(h http.Handler) http.Handler { return h }, logr.Discard()))
	defer srv.Close()
	open := func() float64 { return testutil.ToFloat64(metrics.InspectEventStreams) }
	before := open()
	streams := func() float64 { return testutil.ToFloat64(metrics.InspectRequests.WithLabelValues(streamOperation, "200")) }
	requests := streams()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+BasePath+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode, "the recorder still flushes")
	assert.Eventually(t, func() bool { return open() == before+1 }, time.Second, 10*time.Millisecond)

	cancel()
	assert.Eventually(t, func() bool { return open() == before }, time.Second, 10*time.Millisecond)
	assert.Eventually(t, func() bool { return streams() == requests+1 }, time.Second, 10*time.Millisecond)
}
