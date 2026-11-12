package boundary

import "testing"

func TestCalculateDiscount_Boundary(t *testing.T) {
	got := CalculateDiscount(100)
	want := 10
	if got != want {
		t.Fatalf("CalculateDiscount(100) = %d; want %d", got, want)
	}
}
