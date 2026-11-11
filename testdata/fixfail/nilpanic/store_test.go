package nilpanic

import "testing"

func TestCache(t *testing.T) {
	c := NewCache()
	c.Put("token", "secret123")
	if got := c.Get("token"); got != "secret123" {
		t.Fatalf("got %q, want secret123", got)
	}
}

