package inspect

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/inspect/api"
)

// Events tells readers of the inspect API that something changed, so they know
// when to read again. It says what changed and never what to: the data has one
// way out, the other operations, and this is only when.
//
// Changes are collected for a moment and sent as one batch. A GitOps sync that
// applies fifty grants reaches a reader as one event, and a reader that reads
// again once per event reads once.
type Events struct {
	// Window is how long changes are collected before they are sent.
	// Defaults to 250ms.
	Window time.Duration
	// MaxStreams bounds the open streams. Each holds a connection and a
	// goroutine for as long as a browser tab stays open. Defaults to 100.
	MaxStreams int
	// Heartbeat is how often an idle stream gets a comment line, so that
	// proxies do not close it. Defaults to 25s.
	Heartbeat time.Duration
	// Cache, if set, is watched from Start on: the manager's cache, whose
	// informers the gateway already runs.
	Cache cache.Cache

	mu      sync.Mutex
	pending map[api.Change]bool
	streams map[chan api.ChangeBatch]bool
	wake    chan struct{}
}

func (e *Events) window() time.Duration {
	if e.Window > 0 {
		return e.Window
	}
	return 250 * time.Millisecond
}

func (e *Events) maxStreams() int {
	if e.MaxStreams > 0 {
		return e.MaxStreams
	}
	return 100
}

func (e *Events) heartbeat() time.Duration {
	if e.Heartbeat > 0 {
		return e.Heartbeat
	}
	return 25 * time.Second
}

func (e *Events) init() {
	if e.pending == nil {
		e.pending = map[api.Change]bool{}
		e.streams = map[chan api.ChangeBatch]bool{}
		e.wake = make(chan struct{}, 1)
	}
}

// Publish records that a resource changed. It never blocks.
func (e *Events) Publish(kind api.ChangeKind, name string) {
	e.mu.Lock()
	e.init()
	e.pending[api.Change{Kind: kind, Name: name}] = true
	e.mu.Unlock()
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// PublishServer records that a backend's catalog or discovery changed.
func (e *Events) PublishServer(name string) { e.Publish(api.ChangeKindServer, name) }

// errTooManyStreams is what Subscribe answers once MaxStreams are open.
var errTooManyStreams = fmt.Errorf("too many open event streams")

// Subscribe opens a stream of batches. Call the returned func to close it.
func (e *Events) Subscribe() (<-chan api.ChangeBatch, func(), error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.init()
	if len(e.streams) >= e.maxStreams() {
		return nil, nil, errTooManyStreams
	}
	ch := make(chan api.ChangeBatch, 8)
	e.streams[ch] = true
	return ch, func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.streams[ch] {
			delete(e.streams, ch)
			close(ch)
		}
	}, nil
}

// Start sends what was published, one batch per window, until ctx ends. It
// implements manager.Runnable.
func (e *Events) Start(ctx context.Context) error {
	e.mu.Lock()
	e.init()
	e.mu.Unlock()
	// The manager starts runnables once its caches have synced, so the
	// informers are there to be watched.
	if e.Cache != nil {
		if err := e.Watch(ctx, e.Cache); err != nil {
			return err
		}
	}
	for {
		select {
		case <-ctx.Done():
			e.mu.Lock()
			for ch := range e.streams {
				delete(e.streams, ch)
				close(ch)
			}
			e.mu.Unlock()
			return nil
		case <-e.wake:
		}
		select {
		case <-ctx.Done():
			continue
		case <-time.After(e.window()):
		}
		e.flush()
	}
}

// NeedLeaderElection is false: every replica serves its own readers.
func (e *Events) NeedLeaderElection() bool { return false }

func (e *Events) flush() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.pending) == 0 {
		return
	}
	batch := api.ChangeBatch{Changes: make([]api.Change, 0, len(e.pending))}
	for c := range e.pending {
		batch.Changes = append(batch.Changes, c)
	}
	clear(e.pending)
	sort.Slice(batch.Changes, func(i, j int) bool {
		a, b := batch.Changes[i], batch.Changes[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Name < b.Name
	})
	for ch := range e.streams {
		select {
		case ch <- batch:
		default:
			// A reader this far behind reads everything again anyway;
			// dropping a batch for it costs nothing it would notice.
		}
	}
}

// stream writes a subscription as server-sent events into a pipe, until ctx
// ends or the subscription closes.
func (e *Events) stream(ctx context.Context) (io.ReadCloser, error) {
	ch, cancel, err := e.Subscribe()
	if err != nil {
		return nil, err
	}
	r, w := io.Pipe()
	go func() {
		defer cancel()
		heartbeat := time.NewTicker(e.heartbeat())
		defer heartbeat.Stop()
		// A first line right away: the response starts, and the reader
		// knows the stream is up.
		if _, err := io.WriteString(w, ": connected\n\n"); err != nil {
			_ = w.CloseWithError(err)
			return
		}
		for {
			var frame []byte
			select {
			case <-ctx.Done():
				_ = w.Close()
				return
			case batch, ok := <-ch:
				if !ok {
					_ = w.Close()
					return
				}
				data, _ := json.Marshal(batch)
				frame = fmt.Appendf(nil, "event: changed\ndata: %s\n\n", data)
			case <-heartbeat.C:
				frame = []byte(": keep-alive\n\n")
			}
			if _, err := w.Write(frame); err != nil {
				_ = w.CloseWithError(err)
				return
			}
		}
	}()
	return r, nil
}

// Watch publishes every change the manager's cache sees to the four kinds —
// status included, which the gateway's own reconcilers ignore, because a
// condition turning False is exactly what a person looking at the console
// wants to see.
func (e *Events) Watch(ctx context.Context, c cache.Cache) error {
	for _, w := range []struct {
		obj  client.Object
		kind api.ChangeKind
		// same reports an update that changes nothing a reader shows.
		same func(oldObj, newObj any) bool
	}{
		{&mcpv1alpha1.MCPServer{}, api.ChangeKindServer, onlyDiscoveryTime},
		{&mcpv1alpha1.MCPToolSet{}, api.ChangeKindToolset, nil},
		{&mcpv1alpha1.MCPCapability{}, api.ChangeKindCapability, nil},
		{&mcpv1alpha1.MCPGrant{}, api.ChangeKindGrant, nil},
	} {
		informer, err := c.GetInformer(ctx, w.obj)
		if err != nil {
			return err
		}
		kind, same := w.kind, w.same
		publish := func(obj any) {
			if o, ok := obj.(client.Object); ok {
				e.Publish(kind, o.GetName())
			} else if d, ok := obj.(toolscache.DeletedFinalStateUnknown); ok {
				if o, ok := d.Obj.(client.Object); ok {
					e.Publish(kind, o.GetName())
				}
			}
		}
		if _, err := informer.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
			AddFunc: publish,
			UpdateFunc: func(oldObj, newObj any) {
				o, ok1 := oldObj.(client.Object)
				n, ok2 := newObj.(client.Object)
				// A resync delivers the same version again; nothing changed.
				if ok1 && ok2 && o.GetResourceVersion() == n.GetResourceVersion() {
					return
				}
				if same != nil && same(oldObj, newObj) {
					return
				}
				publish(newObj)
			},
			DeleteFunc: publish,
		}); err != nil {
			return err
		}
	}
	return nil
}

// onlyDiscoveryTime reports an MCPServer update that moves
// status.lastDiscoveryTime and nothing else. The controller writes one after
// every successful discovery, so announcing it would have every reader read
// again once per discovery interval per server, to see the same catalog.
func onlyDiscoveryTime(oldObj, newObj any) bool {
	o, ok1 := oldObj.(*mcpv1alpha1.MCPServer)
	n, ok2 := newObj.(*mcpv1alpha1.MCPServer)
	if !ok1 || !ok2 {
		return false
	}
	if o.Generation != n.Generation || !equality.Semantic.DeepEqual(o.Labels, n.Labels) {
		return false
	}
	before, after := o.Status.DeepCopy(), n.Status.DeepCopy()
	before.LastDiscoveryTime, after.LastDiscoveryTime = nil, nil
	return equality.Semantic.DeepEqual(before, after)
}
