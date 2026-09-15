package controller

import (
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"

	"forgejo.metapatrol.local/platform/mcp-gateway/internal/metrics"
)

func tally(kind, ready string) float64 {
	return testutil.ToFloat64(metrics.Resources.WithLabelValues(kind, ready))
}

// Reconciles of one kind run side by side. Each must add its own object to the
// tally without undoing what the others recorded.
func TestObserveResource_ConcurrentReconcilesDoNotOverwriteEachOther(t *testing.T) {
	const kind = "MCPServerConcurrentTest"
	var wg sync.WaitGroup
	for _, name := range []string{"a", "b", "c", "d"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			observeResource(kind, name, mcpServer(name, "http://x")) // no condition yet: Unknown
			observeResource(kind, name, readyServer(name))
		}()
	}
	wg.Wait()

	assert.Equal(t, 4.0, tally(kind, "True"))
	assert.Equal(t, 0.0, tally(kind, "Unknown"))
	assert.Equal(t, 0.0, tally(kind, "False"), "every status is published, zero included")

	observeResource(kind, "a", nil)
	assert.Equal(t, 3.0, tally(kind, "True"), "a deleted object is no longer counted")
}
