# How peers are ranked

## First and Eligible

Bitcoin Cash Lab records what happens each time a new block reaches your node. Two numbers form the basis of every peer's record:

- **Eligible:** How many block arrivals the peer was connected for.
- **First:** How many times it was credited with delivering the block first.

**First %** is First divided by Eligible. A peer that delivered first for 20 of its 100 observed blocks has a First rate of 20%.

This measures how often a peer wins against the other connections on your node. The ranking then considers both that performance and how much evidence supports it.

The ranking focuses on the last 500 blocks, so a peer that used to perform well cannot keep its place indefinitely on old results. For peers with fewer observations in that window, lifetime performance fills in the missing history.

## Give the evidence some weight

A peer that wins two of its first ten blocks has a 20% First rate. That looks better than a peer with 75 wins across 500 blocks at 15%, but the second has a much stronger track record.

Bitcoin Cash Lab uses the lower bound of a Wilson confidence interval to account for that difference. In practical terms, a high percentage needs enough observations behind it to earn a high rank.

That is why the order in the table can differ from the raw First percentages: the score considers both the results and the evidence supporting them.

## How block delivery is observed

Bitcoin Cash Lab listens for new blocks through BCHN's ZMQ interface and records the arrival time immediately. It then checks BCHN's peer information to identify which connection delivered the new block.

BCHN does not report which peer delivered a block, so Bitcoin Cash Lab watches each peer's received block-message bytes (`cmpctblock` + `block`). Peers whose counters grew since just after the previous block are credited First for the new one; several peers can tie. Nodes that do report `last_block` (Bitcoin Core style) are matched to the block event by that timestamp instead.

If two peers deliver within the same few milliseconds, BCHN's byte counters cannot put one ahead of the other, so both are credited and the block counts as shared. Expect this fairly often. On nodes that report `last_block` (Bitcoin Core style) the timestamp is only to the second: two peers inside the window mean the block before arrived a moment earlier and one of the times belongs to it. Which cannot be told apart, so neither gets a First and the block counts as unclear. Your own counts stand in the dashboard's status line.

Blocks your node fetches while catching up after being offline are not counted: they arrive several a second from many peers and say nothing about who relays a new block quickly.

BCHN itself favours peers that have just been fast. Under BIP152, it asks up to three peers that recently delivered a new block first to send the next one straight away, without waiting to be asked. A peer that is already ahead keeps that edge more easily – part of why a good manual peer tends to stay good, and why a new candidate needs a few blocks before it shows what it can do.

This builds a record of which connections actually bring new blocks to your node first.

## Not every connection serves the same purpose

Wallets, crawlers and indexers connect to your node for different reasons. They are not necessarily there to deliver blocks, so a zero First rate does not mean they are failing at their job.

Bitcoin Cash Lab marks software categories that are not expected to relay blocks.

## From ranking to selection

The ranking updates as new blocks arrive. With rotation enabled, Bitcoin Cash Lab uses these scores to choose candidates for your manual slots.

See [grace period](grace-period.md), [clear lead](swap-margin.md) and [protected peers](protected-peers.md) for what happens after the ranking.

---

[← Back to the README](../README.md)
