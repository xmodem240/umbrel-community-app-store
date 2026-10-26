# Records from my node

# Measurements

These are the original Bitcoin Lab author's records from a **Bitcoin Core** node, kept here for what they say about the idea, not about Bitcoin Cash. **No measurements from a Bitcoin Cash node have been made yet**; your own figures stand in the dashboard.

The author ran Bitcoin Core on Umbrel with `maxconnections=200`, accepting incoming connections over IPv4, IPv6, Tor and I2P – around 190 inbound peers at any time – and measured from 16 September 2026.

## Who delivered first

At first Core ran as it comes. Then rotation was switched on, which keeps proven outbound peers in the manual slots.

| Period | Manual peers | Inbound |
|---|---:|---:|
| Before rotation, 16–18 Sep | 0% | 98% |
| Rotation on, 19–25 Sep | 61% | 33% |
| After a restart, 25 Sep – 1 Oct | 82% | 16% |

The rest came from the outbound peers Core picked itself, between 1% and 6%. Every manual peer was found by rotation among Core's own automatic outbound connections; inbound peers were never touched.

## Finding the few that make a difference

Of 286 automatic outbound peers that were connected for at least 50 blocks, only 14 ever delivered a block first.

That is what makes the ongoing search useful: new candidates keep arriving, most never beat the connections already in place, but now and then one earns a place among them. (Original author's Bitcoin Core node.)

## What the author noticed while solo mining

After a week of rotation, the author saw slightly fewer stale shares in their own mining setup. That gave the peer measurements a practical meaning: less work arriving too late to count.

These are the author's observations from their setup. Your location, ISP and connections shape your results, and finding out what they are is part of the fun of running your own node.

---

[← Back to the README](../README.md)
