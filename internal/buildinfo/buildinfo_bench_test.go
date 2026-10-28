package buildinfo

import "testing"

// BenchmarkReadVCS is the only work "vx --version" does besides starting up.
func BenchmarkReadVCS(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ReadVCS()
	}
}
