package reactive

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 1. Subscribe / publish / receive value / close. Len()/TotalSubs() return to zero.
func TestTopic_BasicLifecycle(t *testing.T) {
	tp := NewTopic[uint32, int]()

	if got := tp.Len(); got != 0 {
		t.Fatalf("fresh topic Len() = %d; want 0", got)
	}

	sub := tp.Subscribe(42)
	if got := tp.Len(); got != 1 {
		t.Errorf("Len after Subscribe = %d; want 1", got)
	}
	if got := tp.TotalSubs(); got != 1 {
		t.Errorf("TotalSubs after Subscribe = %d; want 1", got)
	}

	tp.Publish(42, 7)
	select {
	case v := <-sub.Updates():
		if v != 7 {
			t.Errorf("Updates got %d; want 7", v)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for published value")
	}

	sub.Close()
	if got := tp.Len(); got != 0 {
		t.Errorf("Len after Close = %d; want 0", got)
	}
	if got := tp.TotalSubs(); got != 0 {
		t.Errorf("TotalSubs after Close = %d; want 0", got)
	}
}

// 2. Publish to a key with no subscribers does not panic and does not leak a map entry.
func TestTopic_PublishNoSubscribers(t *testing.T) {
	tp := NewTopic[string, struct{}]()
	tp.Publish("nobody-home", struct{}{}) // must not panic
	if got := tp.Len(); got != 0 {
		t.Errorf("Len = %d after Publish to empty key; want 0", got)
	}
}

// 3. Publish only reaches subscriptions for the matching key.
func TestTopic_KeyIsolation(t *testing.T) {
	tp := NewTopic[uint32, string]()
	subA := tp.Subscribe(1)
	subB := tp.Subscribe(2)
	defer subA.Close()
	defer subB.Close()

	tp.Publish(1, "for-a")

	select {
	case v := <-subA.Updates():
		if v != "for-a" {
			t.Errorf("subA got %q; want %q", v, "for-a")
		}
	case <-time.After(time.Second):
		t.Fatal("subA timed out waiting for its value")
	}

	select {
	case v := <-subB.Updates():
		t.Errorf("subB must not receive a value for key 1; got %q", v)
	case <-time.After(50 * time.Millisecond):
		// ok
	}
}

// 3b. Multiple subscriptions on the same key all receive.
func TestTopic_MultipleSubsSameKey(t *testing.T) {
	tp := NewTopic[uint32, int]()
	s1, s2 := tp.Subscribe(7), tp.Subscribe(7)
	defer s1.Close()
	defer s2.Close()

	tp.Publish(7, 99)

	for i, sub := range []*TopicSub[int]{s1, s2} {
		select {
		case v := <-sub.Updates():
			if v != 99 {
				t.Errorf("sub %d got %d; want 99", i, v)
			}
		case <-time.After(time.Second):
			t.Fatalf("sub %d timed out", i)
		}
	}
}

// 4. Burst publish without a draining reader does not block; freshest value wins.
//    runtime.NumGoroutine() must not balloon.
func TestTopic_BurstPublishFreshestWins(t *testing.T) {
	tp := NewTopic[uint32, int]()
	sub := tp.Subscribe(1)
	defer sub.Close()

	gBefore := runtime.NumGoroutine()

	const N = 10_000
	start := time.Now()
	for i := 0; i < N; i++ {
		tp.Publish(1, i)
	}
	elapsed := time.Since(start)

	// 10k Publish calls into a cap-1 channel must finish well under a second.
	if elapsed > 5*time.Second {
		t.Errorf("burst publish blocked: took %v for %d sends", elapsed, N)
	}

	select {
	case v := <-sub.Updates():
		if v != N-1 {
			t.Errorf("Updates after burst got %d; want %d (freshest wins)", v, N-1)
		}
	case <-time.After(time.Second):
		t.Fatal("no value buffered after burst publish")
	}

	gAfter := runtime.NumGoroutine()
	if gAfter > gBefore+5 {
		t.Errorf("goroutine count ballooned: before=%d after=%d", gBefore, gAfter)
	}
}

// 5. Concurrent churn: subscribe / publish / close on overlapping keys.
//    After all goroutines finish, Len() and TotalSubs() are zero.
func TestTopic_ConcurrentChurn(t *testing.T) {
	tp := NewTopic[uint32, int]()

	const workers = 32
	const iters = 200

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				key := uint32((seed*13 + i*7) % 16)
				sub := tp.Subscribe(key)
				tp.Publish(key, i)
				// Drain occasionally so the channel-drain branch in Publish runs.
				if i%3 == 0 {
					select {
					case <-sub.Updates():
					default:
					}
				}
				sub.Close()
			}
		}(w)
	}
	wg.Wait()

	if got := tp.Len(); got != 0 {
		t.Errorf("Len after churn = %d; want 0", got)
	}
	if got := tp.TotalSubs(); got != 0 {
		t.Errorf("TotalSubs after churn = %d; want 0", got)
	}
}

// 6. Double-close is a no-op (idempotent). No race detector violations either.
func TestTopic_DoubleClose(t *testing.T) {
	tp := NewTopic[uint32, int]()
	sub := tp.Subscribe(1)

	sub.Close()
	sub.Close() // must not panic, must not double-decrement counters

	if got := tp.TotalSubs(); got != 0 {
		t.Errorf("TotalSubs after double-close = %d; want 0", got)
	}
}

// 6b. Concurrent close from two goroutines is race-clean and idempotent.
func TestTopic_ConcurrentClose(t *testing.T) {
	tp := NewTopic[uint32, int]()
	sub := tp.Subscribe(1)

	var done atomic.Int32
	const racers = 8
	var wg sync.WaitGroup
	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func() {
			defer wg.Done()
			sub.Close()
			done.Add(1)
		}()
	}
	wg.Wait()

	if int(done.Load()) != racers {
		t.Errorf("only %d/%d racers returned", done.Load(), racers)
	}
	if got := tp.TotalSubs(); got != 0 {
		t.Errorf("TotalSubs after concurrent close = %d; want 0", got)
	}
}

// Dirty-bit shape: V = struct{} works as a "something changed for K" signal.
func TestTopic_DirtyBit(t *testing.T) {
	tp := NewTopic[uint32, struct{}]()
	sub := tp.Subscribe(1)
	defer sub.Close()

	tp.Publish(1, struct{}{})
	tp.Publish(1, struct{}{}) // coalesced into the same buffered slot

	select {
	case <-sub.Updates():
	case <-time.After(time.Second):
		t.Fatal("expected one dirty-bit signal")
	}

	select {
	case <-sub.Updates():
		t.Fatal("expected at most one buffered signal; got a second")
	case <-time.After(50 * time.Millisecond):
		// ok — coalesced
	}
}
