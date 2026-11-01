// Package calls records the hooks the data providers run.
package calls

import (
	"strings"
	"sync"
)

var (
	mu  sync.Mutex
	log []string
)

func Add(hook string) {
	mu.Lock()
	defer mu.Unlock()
	log = append(log, hook)
}

// Take returns the hooks recorded since the last Take, space-separated.
func Take() string {
	mu.Lock()
	defer mu.Unlock()
	s := strings.Join(log, " ")
	log = nil
	return s
}
