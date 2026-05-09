package reactive

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 1. Subscribe / publish / receive / close. TotalSubs() returns to zero.
func TestBroadcast_BasicLifecycle(t *testing.T) {
	b := NewBroadcast[int]()
	if got := b.TotalSubs(); got != 0 {
		t.Fatalf("fresh broadcast TotalSubs = %d; want 0", got)
	}

	sub := b.Subscribe()
	if got := b.TotalSubs(); got != 1 {
		t.Errorf("TotalSubs after Subscribe = %d; want 1", got)
	}

	b.Publish(42)
	select {
	case v := <-sub.Updates():
		if v != 42 {
			t.Errorf("Updates got %d; want 42", v)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for published value")
	}

	sub.Close()
	if got := b.TotalSubs(); got != 0 {
		t.Errorf("TotalSubs after Close = %d; want 0", got)
	}
}

// 2. Publish with no subscribers does not panic.
func TestBroadcast_PublishNoSubscribers(t *testing.T) {
	b := NewBroadcast[struct{}]()
	b.Publish(struct{}{}) // must not panic
	if got := b.TotalSubs(); got != 0 {
		t.Errorf("TotalSubs after empty publish = %d; want 0", got)
	}
}

// 3. Publish reaches every subscription.
func TestBroadcast_FanOut(t *testing.T) {
	b := NewBroadcast[string]()
	const N = 5
	subs := make([]*BroadcastSub[string], N)
	for i := range subs {
		subs[i] = b.Subscribe()
	}
	defer func() {
		for _, s := range subs {
			s.Close()
		}
	}()

	b.Publish("hello")
	for i, s := range subs {
		select {
		case v := <-s.Updates():
			if v != "hello" {
				t.Errorf("sub %d got %q; want %q", i, v, "hello")
			}
		case <-time.After(time.Second):
			t.Fatalf("sub %d timed out", i)
		}
	}
}

// 4. Burst publish without a reader; freshest value wins; goroutines do not balloon.
func TestBroadcast_BurstPublishFreshestWins(t *testing.T) {
	b := NewBroadcast[int]()
	sub := b.Subscribe()
	defer sub.Close()

	gBefore := runtime.NumGoroutine()

	const N = 10_000
	start := time.Now()
	for i := 0; i < N; i++ {
		b.Publish(i)
	}
	elapsed := time.Since(start)

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

// 5. Concurrent churn: subscribe / publish / close.
//    After all goroutines finish, TotalSubs() is zero.
func TestBroadcast_ConcurrentChurn(t *testing.T) {
	b := NewBroadcast[int]()

	const workers = 32
	const iters = 200

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				sub := b.Subscribe()
				b.Publish(seed*1000 + i)
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

	if got := b.TotalSubs(); got != 0 {
		t.Errorf("TotalSubs after churn = %d; want 0", got)
	}
}

// 6. Double-close is a no-op.
func TestBroadcast_DoubleClose(t *testing.T) {
	b := NewBroadcast[int]()
	sub := b.Subscribe()

	sub.Close()
	sub.Close()

	if got := b.TotalSubs(); got != 0 {
		t.Errorf("TotalSubs after double-close = %d; want 0", got)
	}
}

// 6b. Concurrent close from many goroutines is race-clean and idempotent.
func TestBroadcast_ConcurrentClose(t *testing.T) {
	b := NewBroadcast[int]()
	sub := b.Subscribe()

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
	if got := b.TotalSubs(); got != 0 {
		t.Errorf("TotalSubs after concurrent close = %d; want 0", got)
	}
}
