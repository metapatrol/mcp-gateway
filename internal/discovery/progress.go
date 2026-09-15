package discovery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Progress receives the progress a backend reports for one tool call.
type Progress func(*mcp.ProgressNotificationParams)

type progressKey struct{}

// WithProgress asks for the progress of the tool call made with ctx. The
// backend's notifications reach progress while the call runs, and until shortly
// after it returns; see progressRoute.settle.
func WithProgress(ctx context.Context, progress Progress) context.Context {
	return context.WithValue(ctx, progressKey{}, progress)
}

const (
	// progressSettle is how long a returned call waits for another
	// notification before it takes the last one it saw as the last one sent.
	progressSettle = 25 * time.Millisecond
	// progressSettleMax bounds that wait, for a backend that reports progress
	// after it has answered.
	progressSettleMax = 250 * time.Millisecond
)

// progressRoutes sends each progress notification to the call it belongs to.
//
// The client that makes requests is shared by every caller, and so is its
// notification handler. A call that wants progress is sent with a progress
// token of the gateway's own, unique to it, and the token is what finds the
// call again.
type progressRoutes struct {
	mu     sync.Mutex
	routes map[string]*progressRoute
}

// progressRoute is the progress of one call.
type progressRoute struct {
	progress Progress
	seen     chan struct{}

	mu       sync.Mutex
	complete bool
}

func newProgressRoutes() *progressRoutes {
	return &progressRoutes{routes: map[string]*progressRoute{}}
}

// open registers a call's progress and returns its token and route.
func (p *progressRoutes) open(progress Progress) (string, *progressRoute) {
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	token := "mcp-gate-" + hex.EncodeToString(raw)
	route := &progressRoute{progress: progress, seen: make(chan struct{}, 1)}
	p.mu.Lock()
	p.routes[token] = route
	p.mu.Unlock()
	return token, route
}

func (p *progressRoutes) close(token string) {
	p.mu.Lock()
	delete(p.routes, token)
	p.mu.Unlock()
}

// deliver passes a notification to the call its token names, if that call is
// still registered.
func (p *progressRoutes) deliver(params *mcp.ProgressNotificationParams) {
	token, ok := params.ProgressToken.(string)
	if !ok {
		return
	}
	p.mu.Lock()
	route := p.routes[token]
	p.mu.Unlock()
	if route == nil {
		return
	}
	route.progress(params)
	route.mu.Lock()
	route.complete = params.Total > 0 && params.Progress >= params.Total
	route.mu.Unlock()
	select {
	case route.seen <- struct{}{}:
	default:
	}
}

// settle waits, after the call has returned, for notifications the backend sent
// before its answer.
//
// The SDK's client hands a response to the waiting call as soon as it reads it,
// and notifications to a handler queue of their own, so the answer can arrive
// ahead of the last notifications the backend sent before it. A call that last
// reported itself complete needs no wait. Otherwise the route stays open until
// no notification has come for progressSettle, or progressSettleMax has passed.
func (r *progressRoute) settle() {
	deadline := time.NewTimer(progressSettleMax)
	defer deadline.Stop()
	for {
		r.mu.Lock()
		complete := r.complete
		r.mu.Unlock()
		if complete {
			return
		}
		quiet := time.NewTimer(progressSettle)
		select {
		case <-r.seen:
			quiet.Stop()
		case <-quiet.C:
			return
		case <-deadline.C:
			quiet.Stop()
			return
		}
	}
}
