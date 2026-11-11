package wrongimport

import "testing"

func TestFormatBanner(t *testing.T) {
	got := FormatBanner("  Forge  ")
	want := "=== Forge ==="
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

