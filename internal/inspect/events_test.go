package inspect

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/inspect/api"
)

func startEvents(t *testing.T, e *Events) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = e.Start(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func receive(t *testing.T, ch <-chan api.ChangeBatch) api.ChangeBatch {
	t.Helper()
	select {
	case b := <-ch:
		return b
	case <-time.After(2 * time.Second):
		t.Fatal("no batch arrived")
		return api.ChangeBatch{}
	}
}

func TestEventsCollectsABurstIntoOneBatch(t *testing.T) {
	e := &Events{Window: 50 * time.Millisecond}
	startEvents(t, e)
	ch, cancel, err := e.Subscribe()
	require.NoError(t, err)
	defer cancel()

	for range 50 {
		e.Publish(api.ChangeKindGrant, "coder")
	}
	e.Publish(api.ChangeKindToolset, "web-search")
	e.PublishServer("tickets")

	assert.Equal(t, api.ChangeBatch{Changes: []api.Change{
		{Kind: api.ChangeKindGrant, Name: "coder"},
		{Kind: api.ChangeKindServer, Name: "tickets"},
		{Kind: api.ChangeKindToolset, Name: "web-search"},
	}}, receive(t, ch), "one batch, without duplicates, sorted")

	select {
	case b := <-ch:
		t.Fatalf("a second batch for the same burst: %v", b)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestEventsBoundsTheStreams(t *testing.T) {
	e := &Events{MaxStreams: 2}
	_, c1, err := e.Subscribe()
	require.NoError(t, err)
	_, c2, err := e.Subscribe()
	require.NoError(t, err)
	_, _, err = e.Subscribe()
	assert.ErrorIs(t, err, errTooManyStreams)
	c1()
	c1() // closing twice is harmless
	_, c3, err := e.Subscribe()
	assert.NoError(t, err, "a closed stream frees its place")
	c2()
	c3()
}

func TestEventsOverHTTP(t *testing.T) {
	e := &Events{Window: 20 * time.Millisecond, Heartbeat: time.Hour}
	startEvents(t, e)
	s := &Server{Events: e, Replica: "r"}
	srv := httptest.NewServer(Handler(s, func(h http.Handler) http.Handler { return h }, logr.Discard()))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+BasePath+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	lines := bufio.NewScanner(resp.Body)
	require.True(t, lines.Scan())
	assert.Equal(t, ": connected", lines.Text(), "the stream opens at once")

	e.Publish(api.ChangeKindToolset, "web-search")
	var event, data string
	for lines.Scan() {
		line := lines.Text()
		if v, ok := strings.CutPrefix(line, "event: "); ok {
			event = v
		}
		if v, ok := strings.CutPrefix(line, "data: "); ok {
			data = v
			break
		}
	}
	assert.Equal(t, "changed", event)
	var batch api.ChangeBatch
	require.NoError(t, json.Unmarshal([]byte(data), &batch))
	assert.Equal(t, []api.Change{{Kind: api.ChangeKindToolset, Name: "web-search"}}, batch.Changes)

	// The reader going away closes its subscription.
	cancel()
	assert.Eventually(t, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return len(e.streams) == 0
	}, 2*time.Second, 10*time.Millisecond)
}

func TestEventsHeartbeat(t *testing.T) {
	e := &Events{Heartbeat: 20 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, err := e.stream(ctx)
	require.NoError(t, err)
	lines := bufio.NewScanner(r)
	require.True(t, lines.Scan())
	require.True(t, lines.Scan()) // blank line after ": connected"
	require.True(t, lines.Scan())
	assert.Equal(t, ": keep-alive", lines.Text())
}

func TestEventsUnavailable(t *testing.T) {
	h := Handler(&Server{}, func(h http.Handler) http.Handler { return h }, logr.Discard())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, BasePath+"/events", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	e := &Events{MaxStreams: 1}
	_, closeFirst, err := e.Subscribe()
	require.NoError(t, err)
	defer closeFirst()
	h = Handler(&Server{Events: e}, func(h http.Handler) http.Handler { return h }, logr.Discard())
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, BasePath+"/events", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "beyond MaxStreams")
}

func TestOnlyDiscoveryTime(t *testing.T) {
	at := func(sec int64) *metav1.Time { tm := metav1.Unix(sec, 0); return &tm }
	server := func(tools int, last *metav1.Time) *mcpv1alpha1.MCPServer {
		return &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "s", Generation: 1},
			Status:     mcpv1alpha1.MCPServerStatus{Available: true, DiscoveredTools: tools, LastDiscoveryTime: last},
		}
	}
	assert.True(t, onlyDiscoveryTime(server(3, at(1)), server(3, at(2))), "a routine discovery is not announced")
	assert.False(t, onlyDiscoveryTime(server(3, at(1)), server(4, at(2))), "a new catalog is")
	down := server(3, at(1))
	down.Status.Available = false
	assert.False(t, onlyDiscoveryTime(server(3, at(1)), down), "a backend going down is")
	edited := server(3, at(1))
	edited.Generation = 2
	assert.False(t, onlyDiscoveryTime(server(3, at(1)), edited), "an edited spec is")
	assert.False(t, onlyDiscoveryTime(&mcpv1alpha1.MCPGrant{}, &mcpv1alpha1.MCPGrant{}))
}
