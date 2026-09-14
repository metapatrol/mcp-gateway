// Package mcptest runs real MCP backends in-process for tests.
//
// The gateway's job is to speak MCP to backends it does not control, so tests
// that stub out the protocol would test the stub. These are the SDK's own server
// behind an httptest listener: the handshake, sessions, pagination and
// list_changed notifications are the real ones.
package mcptest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Backend is one MCP server listening on a local port.
type Backend struct {
	Name string
	URL  string

	// Calls counts tool calls that reached this backend.
	Calls atomic.Int64

	// instructions are what the server returns on initialize. They are fixed
	// when a server is installed, so a change takes a Restart — as it does for
	// a real backend.
	instructions string

	server  atomic.Pointer[mcp.Server]
	handler atomic.Pointer[mcp.StreamableHTTPHandler]
	http    *httptest.Server
}

// NewBackend starts a backend exposing the given tools. It is stopped when the
// test ends.
//
// Each tool answers with a text block naming the backend, the tool and the
// arguments it received, so a test can tell which backend a call reached and
// what arrived there.
func NewBackend(t testing.TB, name string, tools ...string) *Backend {
	t.Helper()
	return NewBackendWithInstructions(t, name, "", tools...)
}

// NewBackendWithInstructions starts a backend that gives clients the given
// instructions on initialize.
func NewBackendWithInstructions(t testing.TB, name, instructions string, tools ...string) *Backend {
	t.Helper()
	b := &Backend{Name: name, instructions: instructions}
	b.install(tools)
	b.http = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.handler.Load().ServeHTTP(w, r)
	}))
	b.URL = b.http.URL
	t.Cleanup(b.Close)
	return b
}

// SetInstructions changes the instructions the next Restart installs.
func (b *Backend) SetInstructions(instructions string) { b.instructions = instructions }

func (b *Backend) install(tools []string) {
	server := mcp.NewServer(&mcp.Implementation{Name: b.Name, Version: "test"}, &mcp.ServerOptions{Instructions: b.instructions})
	b.server.Store(server)
	for _, tool := range tools {
		b.AddTool(tool)
	}
	b.handler.Store(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
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
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("%s/%s %s", b.Name, name, string(req.Params.Arguments))}},
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
