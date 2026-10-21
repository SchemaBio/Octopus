package service

import "testing"

func TestEstimateTaskMinutesFromBytes(t *testing.T) {
	tests := []struct {
		name       string
		totalBytes int64
		want       int
	}{
		{name: "unknown size uses default", totalBytes: 0, want: 60},
		{name: "invalid size uses default", totalBytes: -1, want: 60},
		{name: "small data rounds up to one minute", totalBytes: 1, want: 1},
		{name: "five gibibytes takes thirty minutes", totalBytes: 5 * 1024 * 1024 * 1024, want: 30},
		{name: "ten gibibytes takes sixty minutes", totalBytes: 10 * 1024 * 1024 * 1024, want: 60},
		{name: "twenty gibibytes takes two hours", totalBytes: 20 * 1024 * 1024 * 1024, want: 120},
		{name: "partial minute rounds up", totalBytes: 10*1024*1024*1024 + 1, want: 61},
		{name: "beta fastq pair takes fifty six minutes", totalBytes: 9943569006, want: 56},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := estimateTaskMinutesFromBytes(tt.totalBytes); got != tt.want {
				t.Fatalf("estimateTaskMinutesFromBytes(%d) = %d, want %d", tt.totalBytes, got, tt.want)
			}
		})
	}
}
