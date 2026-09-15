package gateway

import (
	"net/http"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/mcptest"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/metrics"
)

// authorizations records the Authorization headers a backend received, by
// JSON-RPC method.
type authorizations struct {
	mu   sync.Mutex
	seen map[string][]string
}

func (a *authorizations) record(r *http.Request) string {
	method := mcptest.RPCMethod(r)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.seen == nil {
		a.seen = map[string][]string{}
	}
	a.seen[method] = append(a.seen[method], r.Header.Get("Authorization"))
	return method
}

func (a *authorizations) all() map[string][]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string][]string{}
	for k, v := range a.seen {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// withMemory adds a backend that lists its tools openly and accepts tool calls
// only with the coding agent's or the nobody's token — engram's shape — and
// grants its tool to both.
func withMemory(e *env, accepted ...string) (*mcptest.Backend, *authorizations) {
	e.t.Helper()
	memory := mcptest.NewBackend(e.t, "memory", "memory_search")
	seen := &authorizations{}
	memory.Guard(func(r *http.Request) int {
		if seen.record(r) != "tools/call" {
			return 0
		}
		for _, token := range accepted {
			if r.Header.Get("Authorization") == "Bearer "+token {
				return 0
			}
		}
		return http.StatusUnauthorized
	})
	e.registerWithAuth("memory", memory.URL, &mcpv1alpha1.BackendAuth{Type: mcpv1alpha1.BackendAuthPassthrough})
	e.toolSets = append(e.toolSets, toolSet("memory", "memory", "memory_search"))
	e.capabilities = append(e.capabilities, capability("memory", "memory"))
	e.grants = append(e.grants,
		grant("coding-agent-memory", "coding-agent", "memory"),
		grant("nobody-memory", "nobody", "memory"))
	e.apply()
	return memory, seen
}

func TestPassthrough_TheCallersTokenReachesTheBackend(t *testing.T) {
	e := newEnv(t, Options{})
	_, seen := withMemory(e, "token-coding-agent")

	res, err := call(e.connect("token-coding-agent"), "memory_search")
	require.NoError(t, err)
	assert.False(t, res.IsError, text(res))
	assert.Equal(t, `memory/memory_search {"q":"x"}`, text(res))

	// The call went out on a session of the caller's own, opened with the
	// caller's token. The catalog reads went out on the shared session, which
	// never carries it.
	got := seen.all()
	assert.Equal(t, []string{"Bearer token-coding-agent"}, got["tools/call"])
	assert.Contains(t, got["initialize"], "Bearer token-coding-agent")
	require.NotEmpty(t, got["tools/list"])
	for _, h := range got["tools/list"] {
		assert.Empty(t, h, "catalog reads carry no caller's token")
	}
}

// The token is the caller's business with the backend. When the backend turns
// it down, the caller is told so, in words it can act on.
func TestPassthrough_ARefusedTokenIsReportedToTheCaller(t *testing.T) {
	e := newEnv(t, Options{})
	memory, _ := withMemory(e, "token-coding-agent")
	before := testutil.ToFloat64(metrics.ToolCalls.WithLabelValues("memory", "memory_search", metrics.CallUnauthorized))

	res, err := call(e.connect("token-nobody"), "memory_search")
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Equal(t, "mcp-gateway: the backend serving this tool refused the caller's credentials (HTTP 401 Unauthorized)", text(res))
	assert.NotContains(t, text(res), memory.URL)
	assert.Equal(t, before+1, testutil.ToFloat64(metrics.ToolCalls.WithLabelValues("memory", "memory_search", metrics.CallUnauthorized)))

	res, err = call(e.connect("token-coding-agent"), "memory_search")
	require.NoError(t, err)
	assert.False(t, res.IsError, "another caller's refusal leaves the backend usable")
}

// Backends not configured for Passthrough never see an agent's token.
func TestBackendsWithoutPassthroughNeverSeeTheCallersToken(t *testing.T) {
	e := newEnv(t, Options{})
	seen := &authorizations{}
	e.artifact.Guard(func(r *http.Request) int { seen.record(r); return 0 })

	res, err := call(e.connect("token-coding-agent"), "artifact_get")
	require.NoError(t, err)
	require.False(t, res.IsError)
	for method, headers := range seen.all() {
		for _, h := range headers {
			assert.Empty(t, h, "%s carried credentials to a backend with auth type None", method)
		}
	}
}
