package gateway

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
)

// changesKeepAlive is how often an idle GET stream gets a comment, so that a
// proxy between the agent and the gateway does not take it for dead.
const changesKeepAlive = 30 * time.Second

// listChanged is the notification a GET stream carries, as a server-sent event.
const listChanged = "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/tools/list_changed\"}\n\n"

// serveChanges answers the GET an agent of a session-based version opens to
// hear about changes. The SDK serves those versions without sessions, and
// refuses that GET: with no session there is nothing to tie the stream to. The
// gateway needs none. The token names the subject, every replica keeps each
// subject's tools in step with the API on its own, and the stream is told
// whenever the subject's tools change — on whichever replica it landed.
//
// The stream carries tools/list_changed and nothing else: the gateway serves
// tools, and sends no requests of its own to an agent.
func (g *Gateway) serveChanges(w http.ResponseWriter, r *http.Request) {
	// MCP 2026-07-28 has no GET stream; its clients listen with
	// subscriptions/listen.
	if r.Header.Get("MCP-Protocol-Version") == ProtocolVersion {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if !acceptsEventStream(r) {
		http.Error(w, "Accept must contain 'text/event-stream'", http.StatusNotAcceptable)
		return
	}
	info := sdkauth.TokenInfoFromContext(r.Context())
	if info == nil || info.UserID == "" {
		http.Error(w, "no subject", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	changes, stop := g.watch(info.UserID)
	defer stop()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	keepAlive := time.NewTicker(changesKeepAlive)
	defer keepAlive.Stop()
	for {
		var err error
		select {
		case <-r.Context().Done():
			return
		case <-changes:
			_, err = fmt.Fprint(w, listChanged)
		case <-keepAlive.C:
			_, err = fmt.Fprint(w, ": keep-alive\n\n")
		}
		if err != nil {
			return
		}
		flusher.Flush()
	}
}

// watch registers a GET stream for a subject. The channel receives a value
// when the subject's tools change; stop unregisters it. While a stream is
// registered the subject's server is not dropped as idle.
func (g *Gateway) watch(subject string) (<-chan struct{}, func()) {
	g.serverFor(subject)
	changes := make(chan struct{}, 1)
	g.mu.Lock()
	ss := g.subjects[subject]
	ss.streams[changes] = struct{}{}
	g.mu.Unlock()
	return changes, func() {
		g.mu.Lock()
		delete(ss.streams, changes)
		ss.lastUsed = time.Now()
		g.mu.Unlock()
	}
}

func acceptsEventStream(r *http.Request) bool {
	for _, v := range r.Header.Values("Accept") {
		for part := range strings.SplitSeq(v, ",") {
			media, _, _ := strings.Cut(part, ";")
			switch strings.TrimSpace(media) {
			case "text/event-stream", "text/*", "*/*":
				return true
			}
		}
	}
	return false
}

// streamsOf counts a subject's open GET streams. For tests.
func (g *Gateway) streamsOf(subject string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ss, ok := g.subjects[subject]; ok {
		return len(ss.streams)
	}
	return 0
}
