// Package torexit knows the addresses Tor exits connect from, so a peer that
// reaches the node through one is not taken for a peer of the exit's operator.
package torexit

import (
	_ "embed"
	"net/netip"
	"strings"
)

// exits.txt is the Tor Project's bulk exit list, one IPv4 address per line,
// fetched when the app is built (see SOURCE.md).
//
//go:embed exits.txt
var exitsTxt string

var exits = func() map[netip.Addr]bool {
	m := map[netip.Addr]bool{}
	for _, l := range strings.Split(exitsTxt, "\n") {
		if a, err := netip.ParseAddr(strings.TrimSpace(l)); err == nil {
			m[a] = true
		}
	}
	return m
}()

// Is reports whether ip was a Tor exit when the app was built.
func Is(ip string) bool {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	return exits[a.Unmap()]
}

// Count is how many exits the list holds.
func Count() int { return len(exits) }
