package discovery

import (
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
)

func TestProgressRoute_Settle(t *testing.T) {
	routes := newProgressRoutes()

	token, route := routes.open(func(*mcp.ProgressNotificationParams) {})
	routes.deliver(&mcp.ProgressNotificationParams{ProgressToken: token, Progress: 3, Total: 3})
	start := time.Now()
	route.settle()
	assert.Less(t, time.Since(start), progressSettle, "a call that reported itself complete does not wait")

	var got []float64
	token, route = routes.open(func(p *mcp.ProgressNotificationParams) { got = append(got, p.Progress) })
	routes.deliver(&mcp.ProgressNotificationParams{ProgressToken: token, Progress: 1})
	go func() {
		time.Sleep(progressSettle / 5)
		routes.deliver(&mcp.ProgressNotificationParams{ProgressToken: token, Progress: 2})
	}()
	start = time.Now()
	route.settle()
	routes.close(token)
	assert.Equal(t, []float64{1, 2}, got, "a notification that trails the answer still arrives")
	assert.Less(t, time.Since(start), progressSettleMax)

	routes.deliver(&mcp.ProgressNotificationParams{ProgressToken: token, Progress: 3})
	assert.Equal(t, []float64{1, 2}, got, "nothing reaches a closed route")
}
