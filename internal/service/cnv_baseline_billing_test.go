package service

import "testing"

func TestBaselineBillingQuantityRoundsInputBytesUpToGiB(t *testing.T) {
	const gib = int64(1024 * 1024 * 1024)
	cases := []struct {
		bytes int64
		want  int
	}{
		{0, 0},
		{1, 1},
		{gib, 1},
		{gib + 1, 2},
		{10 * gib, 10},
	}
	for _, tc := range cases {
		if got := baselineBillingQuantity(tc.bytes); got != tc.want {
			t.Fatalf("baselineBillingQuantity(%d) = %d, want %d", tc.bytes, got, tc.want)
		}
	}
}
