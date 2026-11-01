package reactive

import "time"

// SetSlowClientTimeout sets the slow-client timeout until the returned
// function restores it.
func SetSlowClientTimeout(d time.Duration) (restore func()) {
	prev := slowClientTimeout
	slowClientTimeout = d
	return func() { slowClientTimeout = prev }
}
