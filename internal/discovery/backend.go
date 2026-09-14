package discovery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Backend is a client connection to one MCP server.
//
// There is one session per backend, shared by every caller. The SDK multiplexes
// concurrent requests over it, so a hundred agents calling the same backend do
// not mean a hundred handshakes — and the backend sees one well-behaved client
// instead of a connection per request.
//
// The session is opened on first use and reopened when it is lost: a backend
// that restarted answers the old session ID with 404, and the next call starts a
// new one.
type Backend struct {
	name       string
	endpoint   string
	client     *mcp.Client
	httpClient *http.Client
	timeout    time.Duration

	mu      sync.Mutex
	session *mcp.ClientSession
	closed  bool
}

func newBackend(name, endpoint string, client *mcp.Client, httpClient *http.Client, connectTimeout time.Duration) *Backend {
	return &Backend{
		name:       name,
		endpoint:   endpoint,
		client:     client,
		httpClient: httpClient,
		timeout:    connectTimeout,
	}
}

// Name is the MCPServer this backend belongs to.
func (b *Backend) Name() string { return b.name }

// Endpoint is the URL the backend is reached at.
func (b *Backend) Endpoint() string { return b.endpoint }

// ErrClosed is returned by a Backend that was unregistered.
var ErrClosed = errors.New("backend was unregistered")

// connect returns the live session, opening one if there is none.
//
// The handshake runs on a context detached from the caller's, bounded by the
// connect timeout. Callers queue behind the lock while it runs; if the first of
// them gave up, the handshake would fail for all of them.
func (b *Backend) connect(ctx context.Context) (*mcp.ClientSession, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ErrClosed
	}
	if b.session != nil {
		return b.session, nil
	}

	connectCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), b.timeout)
	defer cancel()
	session, err := b.client.Connect(connectCtx, &mcp.StreamableClientTransport{
		Endpoint:   b.endpoint,
		HTTPClient: b.httpClient,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", b.endpoint, err)
	}
	b.session = session

	// Forget the session once it ends, whichever side ended it, so the next
	// call opens a fresh one instead of failing against a dead one.
	go func() {
		_ = session.Wait()
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.session == session {
			b.session = nil
		}
	}()
	return session, nil
}

// drop discards session if it is still the current one.
func (b *Backend) drop(session *mcp.ClientSession) {
	b.mu.Lock()
	if b.session == session {
		b.session = nil
	}
	b.mu.Unlock()
	_ = session.Close()
}

// errRejected matches the error the SDK's client transport returns for a request
// that never reached the server — the HTTP request itself failed. The SDK does
// not export it; the JSON-RPC code is how its errors compare.
var errRejected = &jsonrpc.Error{Code: -32005}

// sessionGone reports whether err means the request never reached a live
// session: the backend no longer knows the session, the connection was already
// closed, or the HTTP request did not go out. In each case the backend did not
// act on it, so it is safe to send again on a new session. Any other failure may
// have happened after the backend started running a tool, and a tool call is
// not something to repeat on a guess.
func sessionGone(err error) bool {
	return errors.Is(err, mcp.ErrSessionMissing) ||
		errors.Is(err, mcp.ErrConnectionClosed) ||
		errors.Is(err, errRejected)
}

// BackendError returns the JSON-RPC error a backend answered with, if err is
// one. Errors the client produced itself — a request that could not be sent, a
// session that was gone — are not: they say nothing the caller should see.
func BackendError(err error) (*jsonrpc.Error, bool) {
	var wire *jsonrpc.Error
	if !errors.As(err, &wire) || errors.Is(err, errRejected) {
		return nil, false
	}
	return wire, true
}

// withSession runs fn against the live session, and once more against a new
// session if the first one turned out to be gone.
func withSession[T any](ctx context.Context, b *Backend, fn func(*mcp.ClientSession) (T, error)) (T, error) {
	var zero T
	for attempt := 0; ; attempt++ {
		session, err := b.connect(ctx)
		if err != nil {
			return zero, err
		}
		out, err := fn(session)
		if err != nil && attempt == 0 && sessionGone(err) && ctx.Err() == nil {
			b.drop(session)
			continue
		}
		return out, err
	}
}

// ListTools reads the backend's complete tool list, following pagination.
func (b *Backend) ListTools(ctx context.Context) ([]*mcp.Tool, error) {
	return withSession(ctx, b, func(session *mcp.ClientSession) ([]*mcp.Tool, error) {
		var tools []*mcp.Tool
		for tool, err := range session.Tools(ctx, nil) {
			if err != nil {
				return nil, err
			}
			tools = append(tools, tool)
		}
		return tools, nil
	})
}

// CallTool forwards one tool call.
func (b *Backend) CallTool(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	return withSession(ctx, b, func(session *mcp.ClientSession) (*mcp.CallToolResult, error) {
		return session.CallTool(ctx, params)
	})
}

// Close ends the session and refuses further use.
func (b *Backend) Close() error {
	b.mu.Lock()
	session := b.session
	b.session = nil
	b.closed = true
	b.mu.Unlock()
	if session != nil {
		return session.Close()
	}
	return nil
}
