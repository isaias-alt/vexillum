package schemaver_test

import (
	"testing"

	"github.com/isaias-alt/vexillum/internal/schemaver"
)

func TestSupported(t *testing.T) {
	if !schemaver.Supported(0) {
		t.Error("Supported(0) = false, want true")
	}
	for _, v := range []int{1, 2, 3, 4, 999, -1} {
		if schemaver.Supported(v) {
			t.Errorf("Supported(%d) = true, want false", v)
		}
	}
}
