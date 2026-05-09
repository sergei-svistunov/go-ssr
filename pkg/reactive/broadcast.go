package reactive

import "sync"

// Broadcast is a global (un-keyed) pub/sub fan-out. Use it for events that
// concern every connected client: a "users online" counter, a maintenance
// banner, a deploy notification.
//
// Each subscription has a cap-1 channel; Publish is non-blocking and
// freshest-wins. Use V = struct{} for plain "something changed" signals.
//
// All methods are safe for concurrent use.
type Broadcast[V any] struct {
	mu   sync.Mutex
	subs map[*BroadcastSub[V]]struct{}
}

// NewBroadcast creates an empty Broadcast.
func NewBroadcast[V any]() *Broadcast[V] {
	return &Broadcast[V]{subs: make(map[*BroadcastSub[V]]struct{})}
}

// Subscribe registers a new subscription. The returned *BroadcastSub must
// be closed by the caller when done.
func (b *Broadcast[V]) Subscribe() *BroadcastSub[V] {
	s := &BroadcastSub[V]{ch: make(chan V, 1)}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	s.cleanup = func() {
		b.mu.Lock()
		delete(b.subs, s)
		b.mu.Unlock()
	}
	return s
}

// Publish fans out v to every active subscription. Returns immediately;
// never blocks. Old buffered values are replaced so the freshest write
// wins.
func (b *Broadcast[V]) Publish(v V) {
	b.mu.Lock()
	subs := make([]*BroadcastSub[V], 0, len(b.subs))
	for s := range b.subs {
		subs = append(subs, s)
	}
	b.mu.Unlock()
	for _, s := range subs {
		select {
		case <-s.ch:
		default:
		}
		select {
		case s.ch <- v:
		default:
		}
	}
}

// TotalSubs returns the number of active subscriptions. Intended for
// diagnostics and leak tests.
func (b *Broadcast[V]) TotalSubs() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

// BroadcastSub is a single subscription returned by Broadcast.Subscribe.
type BroadcastSub[V any] struct {
	ch        chan V
	cleanup   func()
	closeOnce sync.Once
}

// Updates returns the read-only delivery channel. The channel is never
// closed; use Close() to unregister.
func (s *BroadcastSub[V]) Updates() <-chan V { return s.ch }

// Close removes the subscription from its Broadcast. Safe to call multiple
// times.
func (s *BroadcastSub[V]) Close() {
	s.closeOnce.Do(func() {
		if s.cleanup != nil {
			s.cleanup()
		}
	})
}
