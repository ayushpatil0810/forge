package offbyone

import "testing"

func TestSumRange(t *testing.T) {
	nums := []int{10, 20, 30, 40, 50}
	got := SumRange(nums, 1, 3)
	want := 20 + 30 + 40 // 90
	if got != want {
		t.Fatalf("SumRange(nums, 1, 3) = %d; want %d", got, want)
	}
}
