package main

import (
	"math"
	"sort"
	"time"
)

// stats summarizes a set of durations. The JSON form is in milliseconds.
type stats struct {
	N      int     `json:"n"`
	MinMS  float64 `json:"min_ms"`
	P50MS  float64 `json:"median_ms"`
	P95MS  float64 `json:"p95_ms"`
	MaxMS  float64 `json:"max_ms"`
	MeanMS float64 `json:"mean_ms"`
}

// ms is d in milliseconds, rounded to the microsecond: finer than any timing
// here can be trusted, and it keeps the baseline file short and its diffs
// readable.
func ms(d time.Duration) float64 {
	return math.Round(float64(d)/float64(time.Microsecond)) / 1000
}

// summarize returns the min, median, p95 (nearest rank), max and mean of ds.
func summarize(ds []time.Duration) stats {
	if len(ds) == 0 {
		return stats{}
	}
	sorted := append([]time.Duration(nil), ds...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var sum time.Duration
	for _, d := range sorted {
		sum += d
	}
	return stats{
		N:      len(sorted),
		MinMS:  ms(sorted[0]),
		P50MS:  ms(percentile(sorted, 0.50)),
		P95MS:  ms(percentile(sorted, 0.95)),
		MaxMS:  ms(sorted[len(sorted)-1]),
		MeanMS: ms(sum / time.Duration(len(sorted))),
	}
}

// percentile is the nearest-rank percentile of an ascending slice, except that
// the median of an even count is the mean of the two middle values.
func percentile(sorted []time.Duration, p float64) time.Duration {
	n := len(sorted)
	if p == 0.50 && n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	rank := int(math.Ceil(p*float64(n))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= n {
		rank = n - 1
	}
	return sorted[rank]
}
