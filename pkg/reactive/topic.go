package reactive

import "sync"

// Topic is a keyed in-process pub/sub fan-out. Use it to wake up Subscribe
// goroutines when something they care about changes elsewhere in the
// process. K is the routing key (typically a user id; any comparable type
// works) and V is the payload.
//
// Each subscription has a cap-1 channel. Publish is non-blocking and
// "freshest-wins": if the channel already has a buffered value, the old
// value is dropped before the new one is written. This matters because
// publishers often run inside a database transaction; we never want them to
// block on a slow subscriber, and a missed intermediate value is fine
// because subscribers consume the latest.
//
// Use V = struct{} for the "dirty bit" pattern (publisher signals "something
// changed for K, re-query the source") and V = some concrete type when the
// publisher already has the new value and the subscriber should not need to
// re-fetch (avoids read-your-own-write hazards across transactions).
//
// All methods are safe for concurrent use.
type Topic[K comparable, V any] struct {
	mu   sync.Mutex
	subs map[K]map[*TopicSub[V]]struct{}
}

// NewTopic creates an empty Topic.
func NewTopic[K comparable, V any]() *Topic[K, V] {
	return &Topic[K, V]{subs: make(map[K]map[*TopicSub[V]]struct{})}
}

// Subscribe registers a new subscription for key. The returned *TopicSub
// must be closed by the caller (typically with defer) so the per-key set
// shrinks back to zero on disconnect.
func (t *Topic[K, V]) Subscribe(key K) *TopicSub[V] {
	s := &TopicSub[V]{ch: make(chan V, 1)}
	t.mu.Lock()
	set, ok := t.subs[key]
	if !ok {
		set = make(map[*TopicSub[V]]struct{}, 1)
		t.subs[key] = set
	}
	set[s] = struct{}{}
	t.mu.Unlock()
	s.cleanup = func() {
		t.mu.Lock()
		if set, ok := t.subs[key]; ok {
			delete(set, s)
			if len(set) == 0 {
				delete(t.subs, key)
			}
		}
		t.mu.Unlock()
	}
	return s
}

// Publish fans out v to every subscription registered for key. Returns
// immediately; never blocks on slow subscribers. Old buffered values are
// replaced so the freshest write wins.
func (t *Topic[K, V]) Publish(key K, v V) {
	t.mu.Lock()
	set := t.subs[key]
	subs := make([]*TopicSub[V], 0, len(set))
	for s := range set {
		subs = append(subs, s)
	}
	t.mu.Unlock()
	for _, s := range subs {
		// Drain stale value first so a slow reader still sees v on its next read.
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

// Len returns the number of distinct keys with at least one active
// subscription. Intended for diagnostics and leak tests.
func (t *Topic[K, V]) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.subs)
}

// TotalSubs returns the total number of active subscriptions across all
// keys. Intended for diagnostics and leak tests.
func (t *Topic[K, V]) TotalSubs() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for _, set := range t.subs {
		n += len(set)
	}
	return n
}

// TopicSub is a single subscription returned by Topic.Subscribe.
type TopicSub[V any] struct {
	ch        chan V
	cleanup   func()
	closeOnce sync.Once
}

// Updates returns the read-only delivery channel. Read it in a select with
// ctx.Done(). The channel is never closed; use Close() to unregister.
func (s *TopicSub[V]) Updates() <-chan V { return s.ch }

// Close removes the subscription from its Topic. Safe to call multiple
// times — subsequent calls are no-ops.
func (s *TopicSub[V]) Close() {
	s.closeOnce.Do(func() {
		if s.cleanup != nil {
			s.cleanup()
		}
	})
}
