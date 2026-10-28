package main

import (
	"testing"
	"time"
)

func durations(ms ...int) []time.Duration {
	out := make([]time.Duration, len(ms))
	for i, v := range ms {
		out[i] = time.Duration(v) * time.Millisecond
	}
	return out
}

func TestSummarizeOddCount(t *testing.T) {
	got := summarize(durations(50, 10, 30, 20, 40))
	if got.N != 5 || got.MinMS != 10 || got.P50MS != 30 || got.MaxMS != 50 || got.MeanMS != 30 {
		t.Errorf("unexpected summary: %+v", got)
	}
	if got.P95MS != 50 {
		t.Errorf("p95 of 5 values is the largest (nearest rank), got %v", got.P95MS)
	}
}

func TestSummarizeEvenCountMedianIsTheMiddlePairMean(t *testing.T) {
	got := summarize(durations(10, 20, 30, 40))
	if got.P50MS != 25 {
		t.Errorf("median of 10,20,30,40 = 25, got %v", got.P50MS)
	}
}

func TestSummarizeP95NearestRank(t *testing.T) {
	var ds []time.Duration
	for i := 1; i <= 100; i++ {
		ds = append(ds, time.Duration(i)*time.Millisecond)
	}
	if got := summarize(ds).P95MS; got != 95 {
		t.Errorf("p95 of 1..100 = 95, got %v", got)
	}
}

func TestSummarizeEmpty(t *testing.T) {
	if got := summarize(nil); got.N != 0 {
		t.Errorf("empty input must give an empty summary, got %+v", got)
	}
}

func TestSummarizeDoesNotReorderItsInput(t *testing.T) {
	in := durations(30, 10, 20)
	summarize(in)
	if in[0] != 30*time.Millisecond {
		t.Errorf("summarize sorted the caller's slice: %v", in)
	}
}
