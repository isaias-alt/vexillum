package schemaver_test

import (
	"testing"

	"github.com/isaias-alt/vexillum/internal/schemaver"
)

func TestSupported(t *testing.T) {
	legacy := []int{1, 2, 3}
	for _, v := range []int{0, 1, 2, 3} {
		if !schemaver.Supported(v, legacy...) {
			t.Errorf("Supported(%d) = false, want true", v)
		}
	}
	for _, v := range []int{4, 999, -1} {
		if schemaver.Supported(v, legacy...) {
			t.Errorf("Supported(%d) = true, want false", v)
		}
	}
	if schemaver.Supported(1) {
		t.Error("Supported(1) with no legacy numbers = true, want false")
	}
}
