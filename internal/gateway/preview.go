package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"forgejo.metapatrol.local/platform/mcp-gateway/internal/discovery"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/policy"
)

// Preview is what an agent offered exactly one resolution receives, as the
// gateway writes it: the JSON-RPC responses to server/discover and to
// tools/list, one per page.
type Preview struct {
	Discover  json.RawMessage
	ToolsList []json.RawMessage
}

// errPreviewOnly answers a call to a tool of a preview server, which nothing
// makes: the server only ever lists.
var errPreviewOnly = errors.New("a preview does not call tools")

// Preview answers server/discover and tools/list for the tools res comes to,
// through the server an agent is served by and the handler it reaches: the
// same construction, the same registration of each tool, the same
// instructions, the same HTTP encoding. Nothing is approximated, so what the
// console shows is what an agent holding res would receive from this replica.
func (g *Gateway) Preview(ctx context.Context, res policy.Resolution) (Preview, error) {
	offered := g.offered(res)
	server := g.newServer()
	for name, w := range offered {
		if err := addTool(server, discovery.Exposed(w.tool, name), func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return nil, errPreviewOnly
		}); err != nil {
			return Preview{}, fmt.Errorf("tool %s: %w", name, err)
		}
	}
	instructions := g.instructionsOf(offered)
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			r, err := next(ctx, method, req)
			if d, ok := r.(*mcp.DiscoverResult); ok && err == nil {
				d.Instructions = instructions
			}
			return r, err
		}
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, streamableOptions(nil))

	var out Preview
	var err error
	if out.Discover, err = previewCall(ctx, handler, 1, "server/discover", nil); err != nil {
		return Preview{}, err
	}
	// Each request stands on its own, as it does for an agent speaking MCP
	// 2026-07-28; the pages are numbered from 1.
	cursor := ""
	for id := 1; ; id++ {
		var params map[string]any
		if cursor != "" {
			params = map[string]any{"cursor": cursor}
		}
		page, err := previewCall(ctx, handler, id, "tools/list", params)
		if err != nil {
			return Preview{}, err
		}
		out.ToolsList = append(out.ToolsList, page)
		var next struct {
			Result struct {
				NextCursor string `json:"nextCursor"`
			} `json:"result"`
		}
		if err := json.Unmarshal(page, &next); err != nil || next.Result.NextCursor == "" || next.Result.NextCursor == cursor {
			return out, nil
		}
		cursor = next.Result.NextCursor
	}
}

// previewCall sends one request as an MCP 2026-07-28 client does and returns
// the JSON-RPC response as written.
func previewCall(ctx context.Context, handler http.Handler, id int, method string, params map[string]any) (json.RawMessage, error) {
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    ProtocolVersion,
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "mcp-gateway-inspect", "version": "preview"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", ProtocolVersion)
	req.Header.Set("Mcp-Method", method)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		return nil, fmt.Errorf("%s: %d %s", method, rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	raw := rec.Body.Bytes()
	// Answered as a server-sent event, the response is the data of its
	// message.
	if strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
		for line := range strings.SplitSeq(string(raw), "\n") {
			if data, ok := strings.CutPrefix(line, "data:"); ok {
				raw = []byte(strings.TrimSpace(data))
			}
		}
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("%s: not a JSON-RPC response: %q", method, raw)
	}
	return json.RawMessage(raw), nil
}
