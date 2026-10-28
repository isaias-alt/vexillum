package pending

import (
	"fmt"
	"testing"
)

func BenchmarkAdd(b *testing.B) {
	root := b.TempDir()
	options := []string{"land it", "hold it", "ship it"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Add(root, "mission is done, land it?", options, 1); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkList(b *testing.B) {
	for _, n := range []int{10, 100} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			root := b.TempDir()
			for i := 0; i < n; i++ {
				if _, err := Add(root, fmt.Sprintf("decision %d", i), []string{"yes", "no"}, 1); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				items, skipped, err := List(root)
				if err != nil || len(skipped) != 0 || len(items) != n {
					b.Fatalf("List: %d items, %d skipped, err %v", len(items), len(skipped), err)
				}
			}
		})
	}
}
