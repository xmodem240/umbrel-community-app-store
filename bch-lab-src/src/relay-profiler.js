'use strict';

/**
 * Relay Profiler - the timing-critical core of Bitcoin Cash Lab.
 *
 * Block detection happens EXCLUSIVELY via BCHN ZMQ `pubhashblock`.
 * There is no RPC polling anywhere on this path. The high-resolution
 * timestamp is captured as the very first statement after the ZMQ message
 * arrives, before any parsing, RPC calls, or DB writes - so RPC/DB latency
 * can never pollute the measurement.
 *
 * Right after the timestamp, exactly one getpeerinfo snapshot is taken.
 * Every peer present in it is "Eligible" (it was connected and could in
 * principle have delivered the block). Which peers are "First" depends on what
 * the node reports:
 *
 *  - Bitcoin Core style (`last_block` present): the unix-second timestamp of
 *    the last block received FROM that peer. A peer whose last_block falls
 *    within a short window of the ZMQ detection instant delivered this block.
 *  - BCHN (no `last_block`; read from its source): the peers whose received
 *    block-message bytes (`bytesrecv_per_msg`) grew since a baseline taken
 *    after the previous block. See lib/delivery-bytes.js for the method and
 *    its limits.
 */

const config = require('./lib/config');
const db = require('./lib/db');
const rpc = require('./lib/rpc');
const health = require('./lib/health');
const processGuard = require('./lib/process-guard');
const hashblock = require('./lib/hashblock-subscriber');
const poolId = require('./lib/pool-id');
const stratumRace = require('./lib/stratum-race-toggle');
const deliveryBytes = require('./lib/delivery-bytes');
const logger = require('./lib/logger').make('relay-profiler');

// A peer counts as "first" if BCHN recorded a block from it within this
// many ms of our ZMQ-detection instant. last_block has 1s resolution, so
// this must comfortably straddle a second boundary in either direction.
const FIRST_WINDOW_MS = 2500;

/**
 * Did this peer deliver the block we just detected?
 *
 * The window has to tolerate a second boundary landing anywhere inside our
 * millisecond-precision detection instant, since `last_block` only has
 * one-second resolution. The upper bound is tighter than the lower one on
 * purpose: a peer that delivered the block did so *before* we heard about it,
 * so more than a second into the future is a clock artifact, not a delivery.
 *
 * One function because this used to be written out twice - once for the row
 * that goes into the database, once for the log line - and the two drifted:
 * the log's copy had lost the upper bound and could report a higher "first"
 * count than was actually stored.
 *
 * More than one peer can match, but only ever by mistake. Core sets
 * last_block only when the block was new to it (ProcessBlock, `new_block`), so
 * each block is credited to exactly one connection. Two matches mean another
 * block arrived within the window - on the node this was written for, all
 * three cases were the previous block 1.2 to 2.3 s earlier; on one restarted
 * daily it is hundreds, because the first blocks after Core and this app start
 * together slip past isCatchingUp - and one of the two last_blocks belongs to
 * that other block. Which one cannot be told from here, so neither
 * peer is credited: recordRace keeps the block, stores how many matched in
 * first_count, and the status line counts it as unclear.
 *
 * It also barely happens. Measured on a listening node with ~200 peers, over
 * 1,487 recorded races: 1,486 had exactly one peer inside the window, one had
 * two, and none had zero. Re-measured 458 blocks after the first count, the
 * number of multi-first races had not moved at all - still that one - so the
 * rate is falling as the sample grows rather than holding at some fraction.
 *
 * The zero is the number worth keeping in mind: the window does not merely
 * tolerate the one-second resolution of last_block, it identifies exactly one
 * peer in 99.93% of blocks and misses in none, which is the evidence that the
 * whole method works.
 */
function isFirstPeer(peer, detectedAtMs) {
  if (typeof peer.last_block !== 'number') return false;
  const lastBlockMs = peer.last_block * 1000;
  if (lastBlockMs <= 0) return false;
  return Math.abs(lastBlockMs - detectedAtMs) <= FIRST_WINDOW_MS && lastBlockMs <= detectedAtMs + 1000;
}

/**
 * How far the closest last_block any peer reported sits from the instant we
 * detected the block, signed: negative means BCHN's clocks are behind ours.
 *
 * On a healthy node this is under a second, because the peer that delivered
 * the block has a last_block of right now and the only error is last_block's
 * one-second resolution. It is the diagnostic half of the attribution: when
 * nobody is credited, this says whether that is because the clocks disagree
 * (a steady several seconds, block after block) or for some other reason.
 *
 * Costs one pass over a list already in memory, after the timestamp and after
 * the RPC. Nothing here is on the timing path.
 */
function nearestLastBlockDeltaMs(peers, detectedAtMs) {
  let nearest = null;
  for (const peer of peers) {
    if (typeof peer.last_block !== 'number' || peer.last_block <= 0) continue;
    const delta = peer.last_block * 1000 - detectedAtMs;
    if (nearest === null || Math.abs(delta) < Math.abs(nearest)) nearest = delta;
  }
  return nearest;
}

/**
 * The ping of the peer that delivered the block, from the same snapshot that
 * credited it: the lowest round trip BCHN ever measured to it, which is nearer
 * the line itself than the last one. With two credited (byte-counter method),
 * the shorter line - that is the one the block most plausibly came over. Null
 * when none was credited, when the block was unclear, or BCHN reported no ping.
 */
function firstPeerPingMs(peers, detectedAtMs, firstIds = null) {
  let best = null;
  for (const p of peers) {
    if (!(firstIds ? firstIds.has(p.id) : isFirstPeer(p, detectedAtMs))) continue;
    const s = typeof p.minping === 'number' ? p.minping : (typeof p.pingtime === 'number' ? p.pingtime : null);
    if (s == null) continue;
    const ms = s * 1000;
    if (best == null || ms < best) best = ms;
  }
  return best;
}

/**
 * `firstIds`, when given, is the set of peer ids credited by the byte-counter
 * method (BCHN); without it the Core-style last_block window decides. In that
 * case there is no clock to compare, so nearest_delta_ms stays NULL.
 */
function recordRace({ blockHash, detectedAtMs, peers, firstIds = null }) {
  const database = db.instance;

  const insertRace = database.prepare(
    `INSERT OR IGNORE INTO relay_race (block_hash, block_height, detected_at, first_count, nearest_delta_ms, first_ping_ms)
     VALUES (?, NULL, ?, ?, ?, ?)`,
  );
  const insertObservation = database.prepare(
    `INSERT OR IGNORE INTO relay_observation (race_id, peer_id, eligible, first) VALUES (?, ?, 1, ?)`,
  );

  const isFirst_ = (p) => (firstIds ? firstIds.has(p.id) : isFirstPeer(p, detectedAtMs));
  const firstCount = peers.filter(isFirst_).length;
  // With last_block, more than one match is another block moments apart, not a
  // shared delivery (see isFirstPeer): nobody is credited. With byte counters
  // several peers delivering inside BCHN's acceptance time is normal, and all
  // of them are credited.
  const unclear = !firstIds && firstCount > 1;
  const nearestDeltaMs = firstIds ? null : nearestLastBlockDeltaMs(peers, detectedAtMs);

  const tx = database.transaction(() => {
    const info = insertRace.run(
      blockHash,
      detectedAtMs,
      firstCount,
      nearestDeltaMs === null ? null : Math.round(nearestDeltaMs),
      unclear ? null : firstPeerPingMs(peers, detectedAtMs, firstIds),
    );
    if (info.changes === 0) {
      // Already recorded (duplicate ZMQ delivery / reconnect replay) - skip.
      return null;
    }
    const raceId = info.lastInsertRowid;
    for (const peer of peers) {
      const peerRow = db.getOrCreatePeer(peer.addr);
      const isFirst = !unclear && isFirst_(peer);
      // peer_relay_stats (the rollup peerRanking reads instead of aggregating
      // this table on every request) is updated by a database trigger on this
      // insert, in this same transaction - see db.js. Nothing to do here.
      insertObservation.run(raceId, peerRow.id, isFirst ? 1 : 0);
    }
    return raceId;
  });

  return tx();
}

async function backfillHeightAndPeerCounts(raceId, blockHash) {
  try {
    const header = await rpc.getBlockHeader(blockHash);
    db.instance.prepare(`UPDATE relay_race SET block_height = ? WHERE id = ?`).run(header.height, raceId);
  } catch (err) {
    // Non-critical - height is cosmetic, never blocks the race itself.
    logger.warn('getblockheader failed (non-critical)', { blockHash, error: err.message });
  }
}

const addressesOf = (tx) =>
  (tx.vout || [])
    .map((out) => {
      const spk = out.scriptPubKey || {};
      return spk.address || (Array.isArray(spk.addresses) ? spk.addresses[0] : null);
    })
    .filter(Boolean);

/**
 * Who mined this block, read off its coinbase.
 *
 * Deliberately late and deliberately slow: it runs after the race is already
 * written, it is two RPC calls, and nothing waits for the answer. A block
 * nobody could attribute is recorded as such (pool_source = 'none') so it is
 * never asked about twice; a block this failed on keeps pool_source NULL and
 * the catch-up below picks it up later.
 */
async function attributePool(raceId, blockHash) {
  let name = null;
  let tag = null;
  let source = 'none';
  try {
    const block = await rpc.getBlock(blockHash, 1);
    const coinbaseTxid = Array.isArray(block.tx) ? block.tx[0] : null;
    if (!coinbaseTxid) return;
    const tx = await rpc.getRawTransaction(coinbaseTxid, blockHash);
    const coinbaseHex = (tx.vin && tx.vin[0] && tx.vin[0].coinbase) || '';
    const hit = poolId.identify({ coinbaseHex, addresses: addressesOf(tx) });
    if (hit) {
      name = hit.name;
      tag = hit.tag;
      source = hit.source;
    } else {
      // Nobody we know of. What the miner wrote about himself is still worth
      // showing - as his own words, never as a pool name.
      tag = poolId.coinbaseLabel(coinbaseHex);
      source = tag ? 'coinbase' : 'none';
    }
  } catch (err) {
    logger.warn('could not read the coinbase (non-critical)', { blockHash, error: err.message });
    return;
  }
  try {
    db.instance
      .prepare(`UPDATE relay_race SET pool_name = ?, pool_tag = ?, pool_source = ? WHERE id = ?`)
      .run(name, tag, source, raceId);
    logger.debug('block attributed', { blockHash, pool: name || tag, source });
  } catch (err) {
    logger.warn('could not record the pool (non-critical)', { blockHash, error: err.message });
  }
}

// Blocks recorded while this was not running, or while BCHN was unreachable,
// stay unattributed. Fill in the most recent ones once at startup - bounded,
// because the point is the block somebody is about to look at, not the
// history of the whole database.
const CATCHUP_LIMIT = 20;

async function catchUpAttribution() {
  let rows;
  try {
    rows = db.instance
      .prepare(
        `SELECT id, block_hash AS blockHash FROM relay_race
         WHERE pool_source IS NULL ORDER BY id DESC LIMIT ?`,
      )
      .all(CATCHUP_LIMIT);
  } catch (err) {
    logger.warn('could not look for unattributed blocks', { error: err.message });
    return;
  }
  if (!rows.length) return;
  logger.info('filling in the pool for recent blocks', { blocks: rows.length });
  for (const row of rows) {
    await attributePool(row.id, row.blockHash);
  }
}

/**
 * How long after the block announcement BCHN has a new block template ready -
 * the thing every pool on this node waits for before it can send a job.
 *
 * Asked at the same moment a pool asks, straight off the ZMQ event, and timed
 * to the last byte of the reply; the reply itself is dropped unread (see
 * rpc.timeCall). BCHN serialises template building and keeps the result for
 * a few seconds, so this can only ever make a pool that asks just after it
 * faster, never slower - which is said where the number is shown.
 *
 * Running alongside getpeerinfo does not touch the First measurement: that
 * compares each peer's last_block against the instant of the ZMQ event, both
 * fixed before either call returns.
 */
function timeTemplate(detectedAtMs) {
  return rpc
    .timeCall('getblocktemplate', [{}], { timeoutMs: 30000, count: '"txid"' })
    // How many transactions went into it: a pool spends time on every one, so
    // this is what makes one block's job slower than the next.
    .then(({ count }) => ({ ms: Date.now() - detectedAtMs, tx: count }))
    .catch((err) => {
      logger.warn('block template timing failed (non-critical)', { error: err.message });
      return null;
    });
}

/**
 * Is BCHN catching up rather than following the tip?
 *
 * A node that was offline, or whose chain fell behind, fetches the missing
 * blocks in parallel from many peers, several a second. Every one of them
 * still fires a hashblock, and every one would be scored as a race - but none
 * of them was a race. Which peer BCHN happened to ask for block N says nothing
 * about who relays a new block quickly, and with whole seconds in last_block
 * many peers end up "first" together. Noticed on a friend's node after a
 * resync: a burst of shared Firsts, and a ranking skewed towards whoever had
 * served the backlog.
 *
 * BCHN knows, three ways. The block itself: by the time this asks, a
 * catch-up block already has more blocks on top of it (confirmations > 1) -
 * BCHN connects a backlog faster than these calls return, which is why the
 * chain-wide numbers alone were not enough (tried against a real node: 29 of
 * 30 catch-up blocks still counted). The chain: headers run ahead of blocks
 * while BCHN downloads, and initialblockdownload is set while the tip is old.
 * A block at the tip has one confirmation and headers equal to blocks.
 */
function isCatchingUp(info, header) {
  if (header && Number.isFinite(header.confirmations) && header.confirmations > 1) return true;
  if (!info) return false;
  if (info.initialblockdownload === true) return true;
  return Number.isFinite(info.headers) && Number.isFinite(info.blocks) && info.headers > info.blocks;
}

/**
 * Whether to time the template at all: only beside Stratum Race, the one place
 * the number is read. getblocktemplate builds a whole block in BCHN, and on a
 * node that runs no pool it was CPU spent on every block for nothing.
 *
 * Read from the database like every other process reads the switch, after the
 * timestamp has already been taken. A failed read counts as off.
 */
function stratumRaceOn() {
  try {
    return stratumRace.isEnabled();
  } catch (err) {
    logger.debug('could not read the Stratum Race switch', { error: err.message });
    return false;
  }
}

const CATCHUP_TAIL_MS = 30 * 1000;
let lastCatchUpAt = null;
let lastHashblockAt = null;

// BCHN attribution (lib/delivery-bytes.js): every peer's block-byte counters as
// they stood a while after the previous block. Taken after SETTLE_MS rather than
// at the block itself, so the duplicate copies that high-bandwidth peers send
// moments after a block are already in the baseline and are not credited to the
// next one.
const BASELINE_SETTLE_MS = 20 * 1000;
let baseline = null;
let baselineTimer = null;

async function refreshBaseline(reason) {
  try {
    const peers = await rpc.getPeerInfo();
    if (!deliveryBytes.reportsBlockBytes(peers)) return;
    baseline = deliveryBytes.takeBaseline(peers, Date.now());
    logger.debug('delivery baseline taken', { reason, peers: baseline.bytes.size });
  } catch (err) {
    logger.warn('could not take the delivery baseline', { reason, error: err.message });
  }
}

function scheduleBaseline(reason, delayMs = BASELINE_SETTLE_MS) {
  if (baselineTimer) clearTimeout(baselineTimer);
  baselineTimer = setTimeout(() => { baselineTimer = null; refreshBaseline(reason); }, delayMs);
  if (baselineTimer.unref) baselineTimer.unref();
}

async function handleHashBlock(event) {
  try {
    await handleHashBlockInner(event);
  } finally {
    // Whatever happened to this block - counted, skipped as part of a catch-up,
    // or failed - the next one is measured against how the peers stand a little
    // after it. Only matters to the byte-counter method; harmless otherwise.
    scheduleBaseline('after block');
  }
}

async function handleHashBlockInner({ blockHash, detectedAtMs, t0 }) {
  // Not while catching up: a full block template for every old block is load
  // on BCHN exactly when it is busiest, and measures nothing. The first block
  // of a burst is only known as one afterwards; the rest are skipped.
  // Blocks seconds apart are a backlog too, before BCHN has said so: the
  // first block's questions are still on their way when the next arrives.
  const inCatchUp = (lastCatchUpAt != null && detectedAtMs - lastCatchUpAt < CATCHUP_TAIL_MS)
    || (lastHashblockAt != null && detectedAtMs - lastHashblockAt < 5000);
  lastHashblockAt = detectedAtMs;
  const template = stratumRaceOn() && !inCatchUp ? timeTemplate(detectedAtMs) : Promise.resolve(null);
  let peers;
  let chain;
  let header;
  try {
    // Side by side: the chain questions must not delay the snapshot, and the
    // snapshot is what First is read from. If they fail, they only cannot be
    // asked - the block is then counted as before.
    const quiet = (what) => (err) => {
      logger.warn(`${what} failed - counting the block as usual`, { error: err.message });
      return null;
    };
    [peers, chain, header] = await Promise.all([
      rpc.getPeerInfo(),
      rpc.getBlockchainInfo().catch(quiet('getblockchaininfo')),
      rpc.getBlockHeader(blockHash).catch(quiet('getblockheader')),
    ]);
  } catch (err) {
    logger.error('getpeerinfo failed right after ZMQ hashblock - race lost for this block', {
      blockHash,
      error: err.message,
    });
    return;
  }

  // The block that ends a catch-up is at the tip by every test above - one
  // confirmation, headers equal to blocks - yet it came in the same burst as
  // the backlog, from whichever peer served it. A block that follows a
  // skipped one within CATCHUP_TAIL_MS is treated as part of that burst.
  // Real blocks are ten minutes apart; two found within half a minute right
  // after a catch-up is rare enough not to matter.
  const tailOfCatchUp = lastCatchUpAt != null && detectedAtMs - lastCatchUpAt < CATCHUP_TAIL_MS;
  if (isCatchingUp(chain, header) || tailOfCatchUp) {
    lastCatchUpAt = detectedAtMs;
    logger.info('block not counted - BCHN is catching up', {
      blockHash,
      confirmations: header && header.confirmations,
      blocks: chain && chain.blocks,
      headers: chain && chain.headers,
      initialBlockDownload: !!(chain && chain.initialblockdownload),
    });
    return;
  }

  // Which way to credit First: the node's own last_block when it reports one,
  // otherwise the growth of each peer's block-message bytes since the baseline.
  let firstIds = null;
  if (!deliveryBytes.reportsLastBlock(peers)) {
    if (!deliveryBytes.reportsBlockBytes(peers)) {
      logger.error('this node reports neither last_block nor per-message byte counters - First cannot be measured', { blockHash });
      return;
    }
    if (!baseline) {
      // Just started: nothing to compare against. Counting the block anyway
      // would add an Eligible to every peer and a First to none.
      logger.info('block not counted - no delivery baseline yet (the first block after a start)', { blockHash });
      return;
    }
    firstIds = deliveryBytes.creditedSince(peers, baseline);
  }

  let raceId;
  try {
    raceId = recordRace({ blockHash, detectedAtMs, peers, firstIds });
  } catch (err) {
    // Before v1.12.0 this call sat outside any try/catch inside a
    // fire-and-forget async function, so a SQLite write failing here (a lock
    // held past busy_timeout, a full disk) became an unhandled rejection and
    // terminated the process - the one process whose data cannot be
    // reconstructed afterwards. Losing a single block's race is bad; losing
    // every subsequent block until someone notices is far worse.
    logger.error('failed to record block race', { blockHash, error: err.stack || err.message });
    return;
  }

  const elapsedMs = Number(process.hrtime.bigint() - t0) / 1e6;

  if (raceId == null) {
    logger.debug('duplicate hashblock ignored', { blockHash });
    return;
  }

  const firstCount = firstIds ? firstIds.size : peers.filter((p) => isFirstPeer(p, detectedAtMs)).length;
  const unclear = !firstIds && firstCount > 1;
  logger.info('block race recorded', {
    blockHash,
    eligible: peers.length,
    first: unclear ? 0 : firstCount,
    unclear,
    method: firstIds ? 'block bytes' : 'last_block',
    nearestLastBlockMs: firstIds ? null : nearestLastBlockDeltaMs(peers, detectedAtMs),
    processingMs: Number(elapsedMs.toFixed(2)),
  });

  backfillHeightAndPeerCounts(raceId, blockHash);
  template.then((t) => {
    if (t == null) return;
    try {
      db.instance.prepare(`UPDATE relay_race SET template_ms = ?, template_tx = ? WHERE id = ?`).run(t.ms, t.tx, raceId);
    } catch (err) {
      logger.warn('could not store template time', { error: err.message });
    }
  });
  attributePool(raceId, blockHash).catch((err) =>
    logger.warn('pool attribution failed (non-critical)', { blockHash, error: err.message }),
  );
}

function main() {
  let subscription;
  processGuard.install(logger, { onShutdown: () => subscription && subscription.stop() });
  db.open();
  // The first block after a start has nothing to be measured against unless a
  // baseline exists already.
  scheduleBaseline('startup', 3000);
  const list = poolId.size();
  logger.info('starting', {
    zmq: config.bitcoin.zmqHashBlockUrl,
    poolList: `${list.tags} tags, ${list.addresses} addresses`,
  });

  // Blocks recorded while this was not running have no pool yet. Two seconds
  // in rather than straight away, so a block arriving in the first moments
  // after a restart is never queued behind the history.
  setTimeout(() => {
    catchUpAttribution().catch((err) => logger.warn('catch-up failed', { error: err.message }));
  }, 2000).unref?.();

  subscription = hashblock.start({
    url: config.bitcoin.zmqHashBlockUrl,
    logger,
    // Fire-and-forget so a slow getpeerinfo never delays the next ZMQ message
    // from being read off the socket. The .catch() is the backstop: every
    // error path inside handleHashBlock is already handled, and anything that
    // still escapes gets logged rather than killing the process.
    onBlock: (event) => {
      handleHashBlock(event).catch((err) => {
        logger.error('unexpected error handling hashblock', { error: err.stack || err.message });
      });
    },
  });

  // Block arrivals are ~10 minutes apart with no upper bound, so "nothing
  // happened recently" is a healthy state here. The heartbeat therefore runs
  // on its own clock and reports the ZMQ connection state rather than block
  // activity.
  health.start(db, 'relay-profiler', logger, () => ({
    zmqConnected: subscription.state.connected,
    lastBlockAtMs: subscription.state.lastBlockAtMs,
    // Without this, "no block since the process started" and "a block a
    // moment ago" are the same null to anyone reading the heartbeat.
    subscribedAtMs: subscription.state.startedAtMs,
  }));
}

if (require.main === module) main();

module.exports = {
  recordRace, handleHashBlock, main, isFirstPeer, isCatchingUp, nearestLastBlockDeltaMs, firstPeerPingMs, FIRST_WINDOW_MS,
};
