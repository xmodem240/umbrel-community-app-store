package torexit

import "testing"

func TestList(t *testing.T) {
	// The Tor network has run well over a thousand exits for years; far fewer
	// means the list was not fetched properly.
	if n := Count(); n < 500 {
		t.Fatalf("only %d exits in exits.txt", n)
	}
}

func TestIs(t *testing.T) {
	var first string
	for a := range exits {
		first = a.String()
		break
	}
	for _, c := range []struct {
		ip   string
		want bool
	}{
		{first, true},
		{"::ffff:" + first, true},
		{"192.0.2.1", false}, // documentation range, never an exit
		{"", false},
		{"abc.onion", false},
	} {
		if got := Is(c.ip); got != c.want {
			t.Errorf("Is(%q) = %v, want %v", c.ip, got, c.want)
		}
	}
}
