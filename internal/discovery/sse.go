package discovery

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// sseTransport is the SDK's HTTP+SSE client transport with the event stream
// tied to the session instead of to the context it was connected with.
//
// The SDK opens the stream with the context Connect is given, and that context
// ends when connecting is done: a handshake bounded by a timeout would close
// the stream, and with it the session, the moment it was established. Here the
// context bounds connecting only; once connected, the stream lives until the
// session is closed.
type sseTransport struct {
	mcp.SSEClientTransport
}

func (t *sseTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	streamCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stop := context.AfterFunc(ctx, cancel)
	conn, err := t.SSEClientTransport.Connect(streamCtx)
	if !stop() {
		// ctx ended while connecting, and the stream with it.
		if err == nil {
			_ = conn.Close()
		}
		return nil, context.Cause(ctx)
	}
	if err != nil {
		cancel()
		return nil, err
	}
	return &sseConn{Connection: conn, cancel: cancel}, nil
}

// sseConn ends its event stream when it is closed.
type sseConn struct {
	mcp.Connection
	cancel context.CancelFunc
}

func (c *sseConn) Close() error {
	err := c.Connection.Close()
	c.cancel()
	return err
}
