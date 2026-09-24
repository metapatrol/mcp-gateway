package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agentCall sends what previewCall sends, to the gateway's real endpoint, as
// the subject the token belongs to.
func agentCall(t *testing.T, url, token, method string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": map[string]any{
		"_meta": map[string]any{
			"io.modelcontextprotocol/protocolVersion":    ProtocolVersion,
			"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "mcp-gateway-inspect", "version": "preview"},
			"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		},
	}})
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", ProtocolVersion)
	req.Header.Set("Mcp-Method", method)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	out := string(raw)
	for line := range strings.SplitSeq(out, "\n") {
		if data, ok := strings.CutPrefix(line, "data:"); ok {
			out = strings.TrimSpace(data)
		}
	}
	return out
}

// TestPreviewIsWhatTheAgentReceives: the preview of a subject's resolution is
// byte for byte what the gateway answers that subject — the envelope, the
// server info, the cache scope, the tools and the instructions.
func TestPreviewIsWhatTheAgentReceives(t *testing.T) {
	e := newEnv(t, Options{Instructions: "Use the tools well."})
	res := e.gateway.Policy().Resolve("coding-agent", e.gateway.catalogs)

	p, err := e.gateway.Preview(context.Background(), res)
	require.NoError(t, err)
	require.Len(t, p.ToolsList, 1)

	assert.JSONEq(t, agentCall(t, e.url, "token-coding-agent", "server/discover"), string(p.Discover))
	assert.JSONEq(t, agentCall(t, e.url, "token-coding-agent", "tools/list"), string(p.ToolsList[0]))

	assert.Contains(t, string(p.Discover), "Use the tools well.")
	assert.Contains(t, string(p.Discover), "## source-intelligence")
	assert.Contains(t, string(p.ToolsList[0]), `"cacheScope":"private"`)
	assert.Contains(t, string(p.ToolsList[0]), `"name":"source_search"`)
}

func TestPreviewOfNothing(t *testing.T) {
	e := newEnv(t, Options{})
	p, err := e.gateway.Preview(context.Background(), e.gateway.Policy().Resolve("nobody", e.gateway.catalogs))
	require.NoError(t, err)
	var list struct {
		Result struct {
			Tools []any `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(p.ToolsList[0], &list))
	assert.Empty(t, list.Result.Tools)
}
