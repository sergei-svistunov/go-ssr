package mux

import (
	"io"
	"log"
	"os"
	"testing"
)

// TestMain discards what the mux logs; tests that check a line capture it.
func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}
