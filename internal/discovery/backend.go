package discovery

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"forgejo.metapatrol.local/platform/mcp-gate/internal/credentials"
)

// Backend is a client connection to one MCP server.
//
// The protocol version is the SDK's to negotiate: MCP 2026-07-28 with a backend
// that speaks it, the backend's newest session-based version otherwise. The
// gateway serves agents in 2026-07-28 alone, but backends are not the gateway's
// to choose, and talking to an older one takes no code of its own here.
//
// Requests go out on one client connection per backend, shared by every caller.
// The SDK multiplexes concurrent requests over it, so a hundred agents calling
// the same backend do not mean a hundred handshakes.
//
// A backend with Passthrough auth sends each tool call with the caller's own
// token. Over MCP 2026-07-28 that is all it takes: there are no sessions, and
// the shared connection carries the calls with the caller's token on each
// request. A session-based backend binds a session to the identity that opened
// it, so there each caller token gets a connection of its own for its tool
// calls, and the shared connection only reads the catalog.
//
// Connections are opened on first use and reopened when lost: a session-based
// backend that restarted answers the old session ID with 404, and the next call
// starts a new one.
//
// Change notifications arrive on a connection of their own, the listener, kept
// open in the background. Connecting to listen can hang on a backend that
// accepts subscriptions/listen and never answers, and the SDK waits for that
// answer without a deadline; on its own connection that costs a retry now and
// then, and never a catalog read or a tool call.
type Backend struct {
	name       string
	endpoint   string
	client     *mcp.Client
	httpClient *http.Client
	timeout    time.Duration
	auth       credentials.Auth

	// shared reads the catalog, and carries the tool calls — a Passthrough
	// backend's too, once it is known to speak MCP 2026-07-28.
	shared *conn

	// protocol is what the backend was found to speak on the shared
	// connection: protocolUnknown until that connection has been opened.
	protocol atomic.Int32
	// sharedFailedAt is when opening the shared connection for a Passthrough
	// call last failed, in UnixNano; calls do not try again for a while.
	sharedFailedAt atomic.Int64

	// progress routes the progress notifications of calls that asked for it.
	progress *progressRoutes

	// stopListening ends the listener; nil when nobody listens.
	stopListening context.CancelFunc
	listening     chan struct{}

	mu        sync.Mutex
	closed    bool
	callers   map[[sha256.Size]byte]*callerConn
	lastSweep time.Time
}

// callerConn is the session of one Passthrough caller token.
type callerConn struct {
	*conn
	lastUsed time.Time
	inflight int
}

// conn is one session with the backend: the transport that authenticates it,
// the live session if there is one, and the attempt to open one under way.
type conn struct {
	backend    *Backend
	httpClient *http.Client

	mu      sync.Mutex
	session *mcp.ClientSession
	closed  bool
	// handshake is the attempt to open a session currently under way, if any.
	handshake *handshake
}

// handshake is one attempt to open a session. done is closed when it finishes,
// and session and err hold its outcome from then on.
type handshake struct {
	started time.Time
	done    chan struct{}
	session *mcp.ClientSession
	err     error
}

// What a backend was found to speak.
const (
	protocolUnknown int32 = iota
	protocolSessions
	protocolStateless
)

// statelessProtocol is the first MCP version without sessions.
const statelessProtocol = "2026-07-28"

// sharedRetryInterval is how long Passthrough calls go straight to the caller's
// own connection after the shared connection could not be opened.
const sharedRetryInterval = 30 * time.Second

const (
	// callerIdleTimeout is how long the session of a caller token that makes
	// no calls is kept. Tokens are replaced every so often, and the session of
	// a replaced token is never used again.
	callerIdleTimeout = 5 * time.Minute
	// callerSweepInterval is how often idle caller sessions are looked for.
	callerSweepInterval = time.Minute
)

// newBackend builds a backend. client makes its requests; listenClient, when not
// nil, is the client its listener connects with.
func newBackend(name, endpoint string, auth credentials.Auth, client, listenClient *mcp.Client, httpClient *http.Client, connectTimeout time.Duration) *Backend {
	b := &Backend{
		name:       name,
		endpoint:   endpoint,
		client:     client,
		httpClient: httpClient,
		timeout:    connectTimeout,
		auth:       auth,
		callers:    map[[sha256.Size]byte]*callerConn{},
	}
	b.shared = b.newConn(auth.Credential)
	b.shared.httpClient.Transport.(*authTransport).callers = auth.Passthrough()
	if listenClient != nil {
		ctx, cancel := context.WithCancel(context.Background())
		b.stopListening = cancel
		b.listening = make(chan struct{})
		go func() {
			defer close(b.listening)
			b.listen(ctx, listenClient)
		}()
	}
	return b
}

func (b *Backend) newConn(credential credentials.Credential) *conn {
	return &conn{
		backend:    b,
		httpClient: b.authenticatedClient(credential, nil),
	}
}

// authenticatedClient builds the HTTP client of one connection. wrap, if not
// nil, sits between the credentials and the registry's transport.
func (b *Backend) authenticatedClient(credential credentials.Credential, wrap func(http.RoundTripper) http.RoundTripper) *http.Client {
	base := b.httpClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	if wrap != nil {
		base = wrap(base)
	}
	return &http.Client{
		Transport:     newAuthTransport(base, b.endpoint, credential),
		CheckRedirect: b.httpClient.CheckRedirect,
		Jar:           b.httpClient.Jar,
	}
}

const (
	// listenRetryMin and listenRetryMax bound the wait before the listener
	// connects again after failing to.
	listenRetryMin = 5 * time.Second
	listenRetryMax = 5 * time.Minute
	// listenReconnectDelay is the pause before reconnecting after a listen
	// connection that worked has ended, so a backend that drops streams at
	// once is not reconnected to in a tight loop.
	listenReconnectDelay = time.Second
)

// listen keeps a connection open that hears the backend's change
// notifications, until ctx ends.
//
// Every request of the listener must see response headers within the connect
// timeout. A backend answers a working subscriptions/listen with its headers at
// once and streams from there, so the bound only cuts off the one that never
// answers — which the SDK would otherwise wait on for ever. The stream itself
// has no deadline.
//
// A connection ends when its session does, which is how a session-based
// backend's notification stream failing shows, and when a subscriptions/listen
// stream ends, which the SDK does not report: the session outlives its stream,
// and without this the listener would go on waiting on a session that hears
// nothing.
func (b *Backend) listen(ctx context.Context, client *mcp.Client) {
	retry := listenRetryMin
	for {
		streamEnded := make(chan struct{})
		var once sync.Once
		httpClient := b.authenticatedClient(b.auth.Credential, func(base http.RoundTripper) http.RoundTripper {
			return &headerTimeout{timeout: b.timeout, base: &listenWatch{
				base:  base,
				ended: func() { once.Do(func() { close(streamEnded) }) },
			}}
		})
		connectCtx, cancel := context.WithTimeout(ctx, b.timeout)
		session, err := client.Connect(connectCtx, &mcp.StreamableClientTransport{
			Endpoint:   b.endpoint,
			HTTPClient: httpClient,
		}, nil)
		cancel()

		wait := retry
		if err == nil {
			retry, wait = listenRetryMin, listenReconnectDelay
			sessionEnded := make(chan struct{})
			go func() {
				_ = session.Wait()
				close(sessionEnded)
			}()
			select {
			case <-sessionEnded:
			case <-streamEnded:
			case <-ctx.Done():
			}
			_ = session.Close()
			<-sessionEnded
		} else {
			retry = min(retry*2, listenRetryMax)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// Name is the MCPServer this backend belongs to.
func (b *Backend) Name() string { return b.name }

// Endpoint is the URL the backend is reached at.
func (b *Backend) Endpoint() string { return b.endpoint }

// Passthrough reports whether tool calls carry the caller's own token.
func (b *Backend) Passthrough() bool { return b.auth.Passthrough() }

// ErrClosed is returned by a Backend that was unregistered.
var ErrClosed = errors.New("backend was unregistered")

// ErrNoCaller is returned for a tool call to a Passthrough backend that carries
// no caller token.
var ErrNoCaller = errors.New("the backend passes the caller's token through, and the call carries none")

// connect returns the live session, opening one if there is none.
//
// The handshake runs in its own goroutine and callers wait for it — each no
// longer than its own context and the connect timeout allow. Waiting on the
// handshake rather than running it is the point: the SDK does not bound every
// step of Connect by the context it is given, and a backend that holds a request
// open must cost the caller the timeout, not the goroutine it runs on. A
// reconcile worker blocked there is one fewer for every other backend.
//
// Concurrent callers share one attempt. An attempt still running past the
// timeout is abandoned and the next caller starts afresh. Should the abandoned
// one finish after all, its session is used if no other attempt got there
// first, and closed otherwise.
func (c *conn) connect(ctx context.Context) (*mcp.ClientSession, error) {
	timeout := c.backend.timeout
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClosed
	}
	if c.session != nil {
		session := c.session
		c.mu.Unlock()
		return session, nil
	}
	if c.handshake == nil || time.Since(c.handshake.started) > timeout {
		c.handshake = &handshake{started: time.Now(), done: make(chan struct{})}
		go c.shake(c.handshake)
	}
	h := c.handshake
	c.mu.Unlock()

	wait := time.NewTimer(time.Until(h.started.Add(timeout)))
	defer wait.Stop()
	select {
	case <-h.done:
		if h.err != nil {
			return nil, h.err
		}
		return h.session, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("connecting to %s: %w", c.backend.endpoint, ctx.Err())
	case <-wait.C:
		return nil, fmt.Errorf("connecting to %s: no session within %s", c.backend.endpoint, timeout)
	}
}

// shake performs one handshake and records its outcome.
func (c *conn) shake(h *handshake) {
	b := c.backend
	ctx, cancel := context.WithTimeout(context.Background(), b.timeout)
	defer cancel()
	session, err := b.client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   b.endpoint,
		HTTPClient: c.httpClient,
		// Notifications are the listener's. A session-based backend
		// would otherwise be held to a GET stream per connection.
		DisableStandaloneSSE: true,
	}, nil)

	c.mu.Lock()
	defer c.mu.Unlock()
	defer close(h.done)
	if c.handshake == h {
		c.handshake = nil
	}
	if err != nil {
		h.err = fmt.Errorf("connecting to %s: %w", b.endpoint, err)
		return
	}
	// The backend was unregistered meanwhile, or another attempt already
	// opened a session: this one is surplus.
	if c.closed || c.session != nil {
		h.err = fmt.Errorf("connecting to %s: the attempt was abandoned", b.endpoint)
		go func() { _ = session.Close() }()
		return
	}
	h.session = session
	c.session = session
	if res := session.InitializeResult(); res != nil && c == b.shared {
		if res.ProtocolVersion >= statelessProtocol {
			b.protocol.Store(protocolStateless)
		} else {
			b.protocol.Store(protocolSessions)
		}
	}

	// Forget the session once it ends, whichever side ended it, so the next
	// call opens a fresh one instead of failing against a dead one.
	go func() {
		_ = session.Wait()
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.session == session {
			c.session = nil
		}
	}()
}

// drop discards session if it is still the current one.
func (c *conn) drop(session *mcp.ClientSession) {
	c.mu.Lock()
	if c.session == session {
		c.session = nil
	}
	c.mu.Unlock()
	_ = session.Close()
}

// close ends the session and refuses further use.
func (c *conn) close() error {
	c.mu.Lock()
	session := c.session
	c.session = nil
	c.closed = true
	c.mu.Unlock()
	if session != nil {
		return session.Close()
	}
	return nil
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
//
// A refused credential also never reached the session, but a new session would
// be refused just the same.
func sessionGone(err error) bool {
	if authFailure(err) {
		return false
	}
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

// withSession runs fn against the live session of c, and once more against a
// new session if the first one turned out to be gone.
func withSession[T any](ctx context.Context, c *conn, fn func(*mcp.ClientSession) (T, error)) (T, error) {
	var zero T
	for attempt := 0; ; attempt++ {
		session, err := c.connect(ctx)
		if err != nil {
			return zero, err
		}
		out, err := fn(session)
		if err != nil && attempt == 0 && sessionGone(err) && ctx.Err() == nil {
			c.drop(session)
			continue
		}
		return out, err
	}
}

// ReadCatalog reads the backend's complete tool list, following pagination, and
// the instructions it gave when the session was opened.
func (b *Backend) ReadCatalog(ctx context.Context) (*Catalog, error) {
	// Discovery runs on an interval whether or not anyone calls, which makes
	// it the place to let go of caller sessions nobody uses any more.
	b.sweepCallers(time.Now())
	return withSession(ctx, b.shared, func(session *mcp.ClientSession) (*Catalog, error) {
		var tools []*mcp.Tool
		for tool, err := range session.Tools(ctx, nil) {
			if err != nil {
				return nil, err
			}
			tools = append(tools, tool)
		}
		var instructions string
		if res := session.InitializeResult(); res != nil {
			instructions = res.Instructions
		}
		return NewCatalog(tools, instructions), nil
	})
}

// CallTool forwards one tool call. For a Passthrough backend, ctx must carry the
// caller's token; see credentials.WithCaller. A ctx made by WithProgress
// receives the progress the backend reports while the call runs.
func (b *Backend) CallTool(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	c, release, err := b.connFor(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	if progress, ok := ctx.Value(progressKey{}).(Progress); ok && progress != nil && b.progress != nil {
		token, route := b.progress.open(progress)
		defer b.progress.close(token)
		defer route.settle()
		params.SetProgressToken(token)
	}
	return withSession(ctx, c, func(session *mcp.ClientSession) (*mcp.CallToolResult, error) {
		return session.CallTool(ctx, params)
	})
}

// connFor picks the session a tool call goes out on, and returns the function to
// call once it is done.
//
// Caller sessions are keyed by a hash of the token, not by the gateway's
// subject. The backend decides for itself who a token belongs to, and two
// tokens the gateway maps to one subject may well be two identities to it — a
// session opened with one would refuse the other.
//
// A Passthrough backend known to speak MCP 2026-07-28 needs none of that: its
// calls go out on the shared connection, which sends the caller's token with
// each. One whose protocol is not known yet is asked on the shared connection
// first; if that connection cannot be opened — a backend that wants a token
// even to be discovered, and no discovery credential — the caller's own
// connection is the way in.
func (b *Backend) connFor(ctx context.Context) (*conn, func(), error) {
	if !b.auth.Passthrough() {
		return b.shared, func() {}, nil
	}
	authorization, ok := credentials.Caller(ctx)
	if !ok {
		return nil, nil, ErrNoCaller
	}
	if b.protocol.Load() == protocolUnknown && time.Since(time.Unix(0, b.sharedFailedAt.Load())) > sharedRetryInterval {
		if _, err := b.shared.connect(ctx); err != nil {
			b.sharedFailedAt.Store(time.Now().UnixNano())
		}
	}
	if b.protocol.Load() == protocolStateless {
		return b.shared, func() {}, nil
	}
	key := sha256.Sum256([]byte(authorization))
	now := time.Now()

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, nil, ErrClosed
	}
	c, ok := b.callers[key]
	if !ok {
		c = &callerConn{conn: b.newConn(credentials.Forward(authorization))}
		b.callers[key] = c
	}
	c.inflight++
	c.lastUsed = now
	release := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		c.inflight--
		c.lastUsed = time.Now()
	}
	return c.conn, release, nil
}

// sweepCallers closes the caller sessions that have been idle too long.
func (b *Backend) sweepCallers(now time.Time) {
	b.mu.Lock()
	if now.Sub(b.lastSweep) < callerSweepInterval {
		b.mu.Unlock()
		return
	}
	b.lastSweep = now
	var idle []*conn
	for key, c := range b.callers {
		if c.inflight == 0 && now.Sub(c.lastUsed) > callerIdleTimeout {
			delete(b.callers, key)
			idle = append(idle, c.conn)
		}
	}
	b.mu.Unlock()
	for _, c := range idle {
		go func() { _ = c.close() }()
	}
}

// callerSessions is the number of caller sessions held, for tests.
func (b *Backend) callerSessions() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.callers)
}

// Close ends every session and the listener, and refuses further use.
func (b *Backend) Close() error {
	b.mu.Lock()
	b.closed = true
	callers := b.callers
	b.callers = map[[sha256.Size]byte]*callerConn{}
	b.mu.Unlock()
	if b.stopListening != nil {
		b.stopListening()
	}
	for _, c := range callers {
		_ = c.close()
	}
	return b.shared.close()
}
