package mux

import "testing"

func TestTernaryIf(t *testing.T) {
	if TernaryIf(true, "yes", "no") != "yes" {
		t.Fatal("TernaryIf(true) should return first value")
	}
	if TernaryIf(false, "yes", "no") != "no" {
		t.Fatal("TernaryIf(false) should return second value")
	}
	if TernaryIf(true, 1, 2) != 1 {
		t.Fatal("TernaryIf(true) should return first int")
	}
}
