// Package mcptest runs real MCP backends in-process for tests.
//
// The gateway's job is to speak MCP to backends it does not control, so tests
// that stub out the protocol would test the stub. These are the SDK's own server
// behind an httptest listener: the handshake, sessions, pagination and
// list_changed notifications are the real ones.
package mcptest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Backend is one MCP server listening on a local port.
type Backend struct {
	Name string
	URL  string

	// Calls counts tool calls that reached this backend.
	Calls atomic.Int64

	// Connections counts the TCP connections clients opened to this backend.
	Connections atomic.Int64

	// instructions are what the server returns on initialize. They are fixed
	// when a server is installed, so a change takes a Restart — as it does for
	// a real backend.
	instructions string

	// stateless serves MCP 2026-07-28, which has no sessions, in addition to
	// the session-based versions a stateful handler speaks.
	stateless bool

	server  atomic.Pointer[mcp.Server]
	handler atomic.Pointer[mcp.StreamableHTTPHandler]
	http    *httptest.Server

	guard atomic.Pointer[func(*http.Request) int]
	users atomic.Pointer[map[string]string]
}

// NewBackend starts a backend exposing the given tools. It is stopped when the
// test ends.
//
// Each tool answers with a text block naming the backend, the tool and the
// arguments it received — and the user, behind RequireBearer — so a test can
// tell which backend a call reached and what arrived there.
func NewBackend(t testing.TB, name string, tools ...string) *Backend {
	t.Helper()
	return NewBackendWithInstructions(t, name, "", tools...)
}

// NewStatelessBackend starts a backend that speaks MCP 2026-07-28: no sessions,
// notifications over subscriptions/listen. A session-based client still gets
// through, statelessly.
func NewStatelessBackend(t testing.TB, name string, tools ...string) *Backend {
	t.Helper()
	return newBackend(t, &Backend{Name: name, stateless: true}, tools)
}

// NewBackendWithInstructions starts a backend that gives clients the given
// instructions on initialize.
func NewBackendWithInstructions(t testing.TB, name, instructions string, tools ...string) *Backend {
	t.Helper()
	return newBackend(t, &Backend{Name: name, instructions: instructions}, tools)
}

func newBackend(t testing.TB, b *Backend, tools []string) *Backend {
	t.Helper()
	b.install(tools)
	b.http = httptest.NewUnstartedServer(http.HandlerFunc(b.serve))
	b.http.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			b.Connections.Add(1)
		}
	}
	b.http.Start()
	b.URL = b.http.URL
	t.Cleanup(b.Close)
	return b
}

func (b *Backend) serve(w http.ResponseWriter, r *http.Request) {
	if guard := b.guard.Load(); guard != nil {
		if status := (*guard)(r); status != 0 {
			http.Error(w, http.StatusText(status), status)
			return
		}
	}
	var h http.Handler = b.handler.Load()
	if users := b.users.Load(); users != nil {
		h = sdkauth.RequireBearerToken(func(_ context.Context, token string, _ *http.Request) (*sdkauth.TokenInfo, error) {
			user, ok := (*users)[token]
			if !ok {
				return nil, sdkauth.ErrInvalidToken
			}
			return &sdkauth.TokenInfo{UserID: user, Expiration: time.Now().Add(time.Hour)}, nil
		}, nil)(h)
	}
	h.ServeHTTP(w, r)
}

// Guard puts check in front of every request the backend receives. A request
// check returns a status other than 0 for is refused with that status. check
// may read the body; RPCMethod does so without consuming it.
func (b *Backend) Guard(check func(r *http.Request) int) { b.guard.Store(&check) }

// RequireBearer makes the backend demand a bearer token from users, which maps
// tokens to the users they belong to. It uses the SDK's own middleware, so a
// session is bound to the user that opened it, as it is on a real backend, and
// tool results name the user who called.
func (b *Backend) RequireBearer(users map[string]string) { b.users.Store(&users) }

// RPCMethod returns the JSON-RPC method of a POST, leaving the body readable.
// It is "" for anything else.
func RPCMethod(r *http.Request) string {
	if r.Method != http.MethodPost || r.Body == nil {
		return ""
	}
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	var msg struct {
		Method string `json:"method"`
	}
	_ = json.Unmarshal(body, &msg)
	return msg.Method
}

// SetInstructions changes the instructions the next Restart installs.
func (b *Backend) SetInstructions(instructions string) { b.instructions = instructions }

func (b *Backend) install(tools []string) {
	server := mcp.NewServer(&mcp.Implementation{Name: b.Name, Version: "test"}, &mcp.ServerOptions{Instructions: b.instructions})
	b.server.Store(server)
	for _, tool := range tools {
		b.AddTool(tool)
	}
	b.handler.Store(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: b.stateless}))
}

// Server is the SDK server currently answering.
func (b *Backend) Server() *mcp.Server { return b.server.Load() }

// AddTool adds a tool, or replaces one of the same name. Connected clients are
// notified.
func (b *Backend) AddTool(name string) {
	b.Server().AddTool(&mcp.Tool{
		Name:        name,
		Description: fmt.Sprintf("%s from %s", name, b.Name),
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		b.Calls.Add(1)
		text := fmt.Sprintf("%s/%s %s", b.Name, name, string(req.Params.Arguments))
		if req.Extra != nil && req.Extra.TokenInfo != nil {
			text += " as " + req.Extra.TokenInfo.UserID
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: text}},
		}, nil
	})
}

// AddFailingTool adds a tool that reports a tool-level error.
func (b *Backend) AddFailingTool(name string) {
	b.Server().AddTool(&mcp.Tool{
		Name:        name,
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		b.Calls.Add(1)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "it broke"}},
		}, nil
	})
}

// RemoveTool removes tools. Connected clients are notified.
func (b *Backend) RemoveTool(names ...string) {
	b.Server().RemoveTools(names...)
}

// Restart replaces the server behind the same URL with a fresh one exposing
// the given tools. Every session the old one held is unknown to the new one,
// which is how a backend pod that was rescheduled behind a stable Service looks.
func (b *Backend) Restart(tools ...string) {
	b.install(tools)
	b.http.CloseClientConnections()
}

// Close stops the listener. Sessions end with it, which is how a crashed
// backend looks from the outside.
func (b *Backend) Close() {
	b.http.CloseClientConnections()
	b.http.Close()
}
