# exits.txt

The Tor Project's bulk exit list, <https://check.torproject.org/torbulkexitlist>:
the IPv4 addresses Tor exits connect from, one per line. Current list: fetched
2026-11-11, 1252 addresses.

It is bundled rather than fetched at run time, so Peer Map still makes no
connection of its own to the internet. The price is age: an exit that started
after the build is not known, and its peers simply get no "Tor exit" note. Most
exits run for months, so a list from the last release is close enough.

## Refresh

Before every release:

```sh
curl -sf https://check.torproject.org/torbulkexitlist \
  | grep -E '^[0-9]{1,3}(\.[0-9]{1,3}){3}$' \
  | sort -u -t. -k1,1n -k2,2n -k3,3n -k4,4n > torexit/exits.txt
go test ./torexit/
```

and update the date and count above.

## What the list cannot answer

It holds IPv4 only, so a peer reaching the node from an IPv6 exit gets no note.
