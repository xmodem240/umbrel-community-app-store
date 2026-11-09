# Changelog

## 0.5.7

- The Peer Map check is off by default. There is no Bitcoin Cash Peer Map, and the check looked up a container that does not exist every 30 seconds. `PEERMAP_HEALTH_URL` turns it on.
- Docs and the store description brought up to date (AxePool, official node, stratum login forms, node selection).

## 0.5.6

- Start-up setup restored to that of 0.5.2 (no Umbrel dependency, no extra environment): 0.5.3-0.5.5 would not start on Umbrel. The official-node choice is kept; its RPC user and password are typed in.

## 0.5.5

- Stratum Race: **AxePool** is now offered with the other local Umbrel pools (button, and it is found automatically when installed).

## 0.5.3

- New choice on the Node card: **Bitcoin Cash Node (official Umbrel app)**, the node AxePool uses. Umbrel passes its address, RPC login and ZMQ port to this app, so selecting it needs no typing. Pick it, press Save, and the four services restart onto it.

## 0.5.2

From upstream Bitcoin Lab 2.3.0:

- **Security: a request line that is no URL (for example `//`) no longer stops the dashboard.** It threw outside
  the error handling and the process guard ended the app, a restart loop for anyone who kept sending it. It is
  now answered with 400.
- Stratum Race takes at most 20 pools; each is a socket held open and a row per block.
- The pool login no longer names the app: the worker name is a plain `.worker1` (it was `.bchlab`).
- A peer you disconnect by hand gets a line in the rotation log.
- Only the newest block's peers are tinted, and the block wave runs evenly across the page.

Not taken over: upstream's new tab icon (carries a Bitcoin sign).

## 0.5.1

- The dashboard shows the running version (for example v0.5.1) beside the app name, so it is plain that an update took.

## 0.5.0

From upstream Bitcoin Lab 2.1.1 to 2.2.0:

- **Security: Node 22 base image moved to its current digest** (security fixes).
- Home-screen widget: the four-tile widget is gone (on a fresh install two tiles were empty and one showed a
  peer's address); the traffic widget now shows sent today, received today and peers in / out, refreshed every
  minute.
- Stratum Race shows the latest block and the average instead of median and P90, and says a pool app works
  without a miner.
- The ranking marks disguised peers and pool nodes, and offers no manual slot to a peer that can never deliver.
- Phone legend explaining the marks; a tap on an open explanation folds it shut; one widget shows traffic and
  your peers in and out; Storage shows the database size while folded.
- Shorter rotation notes; the rotation log folds runs of empty kicks; tables drop a column before a type pill
  is cut.
- Dashboard answers are gzip-packed, so pages load faster.

## 0.4.0

From upstream Bitcoin Lab 2.1.0:

- **Security: rebuilt Node 22.23.4 base image** with the latest Debian security updates (the image is pinned
  by digest, so it only changes when the digest is moved; it has now been moved).
- Cards fold to their title and stay folded on this device; the page holds your place while the ranking
  re-sorts; a jump bar takes you to Peers, Rotation, Stratum or Traffic.
- Rows open on a tap to show their buttons (on hover with a mouse); all phone cards have the same height;
  "never first" now stands in the First % column; the Total Time column is gone; the rotation rules wait
  behind "How it works"; a live dot beats on each fresh answer; new peers light up once.
- Bitcoin Purity (a Knots fork) reads as another chain.
- The Node and Logs cards keep their own fold; they are not part of the title-fold.

## 0.3.1

From upstream Bitcoin Lab 2.0.6:

- A reload (or Back) puts you back where you were on the page, instead of at the top.
- LICENSE: copyright line as upstream now writes it.

Not taken over: 2.0.6's extra list of Bitcoin mining-pool names (ckpool-run pools). It is Bitcoin-only and
could mislabel a Bitcoin Cash block.

## 0.3.0

Brings in the upstream Bitcoin Lab changes from 1.26.0 up to 2.0.5 (merged file by file, with the BCH changes kept).

- **New dashboard design:** four tiles in the header, tables that turn into cards on phones, "never first"
  instead of a strike-through, First % shown as the rate over the last 500 blocks, only Storage folds.
- **⚡ and 📡 markers:** ⚡ is a peer BCHN has chosen to send it new blocks unasked (high-bandwidth); 📡 is a peer
  that has chosen *your* node that way. The heading counts them.
- **Reliability:** the live block stream could fill its 32 slots behind Umbrel's app proxy and then show no more
  blocks; streams now end after 10 minutes and reconnect. An RPC call no longer hangs when the node restarts
  mid-reply. A busy database on one tick no longer stops the Stratum worker. Blocks arriving seconds apart no
  longer trigger a template request. Stricter checks on what the dashboard accepts.
- **Rotation:** no kicks unless most recent blocks were credited to someone, and at most two per pass; a manual
  peer replaced while still connected is not parked; a returning parked peer is added before the weakest leaves
  (a refused add no longer empties the slot); parked peers are not probed directly when the node uses a proxy;
  the rotation log records every manual add.
- **Storage:** inbound peers that never delivered first are forgotten after 30 days away
  (`NEVER_FIRST_PEER_RETENTION_DAYS`).
- **Shared vs unclear:** on BCHN, peers that deliver within the same few milliseconds are all credited and the
  status line counts those blocks as "shared"; "unclear" is for nodes that report `last_block`, where two matches
  mean neither is credited.
- Docs: measurements and "my node" remarks in the docs came from the original author's Bitcoin Core node. They are
  now attributed to it, and `docs/measured.md` says no Bitcoin Cash measurements exist yet.
- Docker base image updated to the Node 22 build with the October 2026 security fixes.

## 0.2.2

- Fix: log flooded with "pool rejected mining.authorize ... Invalid address" for the 2Miners pools. 2Miners'
  BCH pools only accept a CashAddr login (`bitcoincash:q...`) and refused the legacy form. After a rejection
  the next connection now offers the other spelling of the same address, and keeps whichever is accepted.
- Fix: the reconnect delay was reset on every connect, so a pool that dropped us on login was retried every
  2 s forever. It now resets only once a login is accepted or a job arrives, and a pool that refuses every
  spelling is retried about every 10 minutes.
- A repeated rejection is logged on the first two attempts, then at most every 30 minutes per pool.
- A miss that cannot be saved because its pool was removed mid-race is no longer a warning.

## 0.2.1

- New **Logs** card on the dashboard: every service's recent log lines (block races recorded, peers, pool
  timings, warnings) with service / level / search filters, auto-refresh and Copy. Each service keeps a
  rotated log file under `/data/logs` (about 8 MB at most); not shown on a read-only copy.

## 0.2.0

- **Fix: works on BCHN.** BCHN's `getpeerinfo` has no `last_block`, so no peer was ever credited
  First (FIRST % stayed 0, red "no peer credited" banner). First is now credited from per-peer
  block-message byte counters when `last_block` is absent.
- `connection_type`, `network` and `servicesnames` are derived when BCHN omits them, so peers are no
  longer typed "unknown" and the rotation can tell manual/inbound/outbound apart.
- Fix: every BCHN peer was labelled "a client of another chain" because of its `(EB32.0)` user agent.
- `scripts/check-node.js` reports which attribution method the node supports.
- After upgrading, use Storage -> Reset peer data: earlier races have no First credits.

## 0.1.1

- AxeBCH preset corrected from WillItMod's own compose files: node `willitmod-dev-bch_bchn_1`, RPC 28332, user `bch`,
  ZMQ 28334 (was a guess at 8332/28332). Only the per-install RPC password has to be entered.
- AxeBCH local pool is `willitmod-dev-bch_ckpool_1:3333` (was a wrong guess).

## 0.1.0

First Bitcoin Cash port, derived from the MIT-licensed Bitcoin Lab by benunskilled
(https://github.com/benunskilled/bitcoin-lab). Not yet run against a live BCHN node.

- Node defaults for BCHN (RPC 8332, ZMQ hashblock 28332); network names for mainnet,
  testnet3, testnet4, scalenet, chipnet and regtest.
- getblocktemplate timing no longer asks for the `segwit` rule, which BCH does not have.
- Stratum Race defaults replaced with BCH pools whose endpoints are published on the
  pools' own setup pages (2Miners Solo BCH, Kryptex BCH).
- Local pool discovery looks for LoneStrike Cash instead of the Bitcoin solo-pool apps.
- Pool attribution list is empty by default (the upstream list is Bitcoin-only); supply
  your own with MINING_POOLS_FILE.
- Added scripts/check-node.js to verify a BCHN node returns the RPC fields the app reads.
- Node selection on the dashboard (Node card): LoneStrike Labs BCHN app, AxeBCH (WillItMod,
  unverified) or any BCHN-compatible node. Test, auto-detect, and apply-by-restart. Saved in
  `node-profile.json` (0600); the password is never sent to the browser.
- AxeBCH added to local Stratum Race pool detection (unverified container name, stratum 4567).
- Dashboard accent colour changed to Bitcoin Cash green.
