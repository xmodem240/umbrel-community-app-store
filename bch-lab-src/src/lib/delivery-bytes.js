'use strict';

/**
 * Who delivered a block, for a node that does not say.
 *
 * Bitcoin Core's getpeerinfo has `last_block`, the time of the last NEW block a
 * peer sent, and the app credits whoever's is closest to the ZMQ instant. BCHN
 * keeps that number internally (nLastBlockTime) and never reports it. What it
 * does report is `bytesrecv_per_msg`: how many bytes of each message type each
 * peer has sent us. A block arrives as a `cmpctblock` (compact block) or a
 * `block`, and ordinary relay is `inv`/`tx`, so the bytes of those two types
 * only move when a peer hands over a block.
 *
 * So: keep a BASELINE of every peer's block bytes, taken a little after the
 * previous block (once the late duplicates of that block have arrived), and at
 * the next block credit every peer whose block bytes have grown since. The
 * snapshot is taken right after BCHN accepted the block, so what has grown is
 * what arrived before acceptance - the peer that delivered it, plus any
 * high-bandwidth peer whose copy landed within the same few milliseconds.
 * Several peers can be credited; that is the same "tie" the Core method has
 * and the dashboard already counts.
 *
 * Honest limits:
 *  - It says "delivered block data before BCHN accepted the block", which is
 *    what First is for, but it cannot order peers whose copies arrive within
 *    the acceptance time of each other. Expect more ties than with last_block.
 *  - A competing or stale block relayed in between would also move the counter
 *    and be credited to the next block.
 *  - After a restart there is no baseline until a block has passed, so that
 *    one block is not counted rather than counted wrongly.
 */

// Message types that carry a block to us. `blocktxn` is the follow-up to a
// `cmpctblock` from the same peer, so it adds nothing.
const BLOCK_MESSAGES = ['cmpctblock', 'block'];

// The smallest block message is a header plus a coinbase: well over this.
// Guards against crediting on noise should a counter ever move by a few bytes.
const MIN_DELIVERY_BYTES = 80;

/** Bytes of block messages this peer has sent us, or null if the node does not report it. */
function blockBytes(peer) {
  const perMsg = peer && peer.bytesrecv_per_msg;
  if (!perMsg || typeof perMsg !== 'object') return null;
  let total = 0;
  for (const type of BLOCK_MESSAGES) {
    const n = Number(perMsg[type]);
    if (Number.isFinite(n) && n > 0) total += n;
  }
  return total;
}

/** True if the node reports `last_block` for its peers (Core-style attribution applies). */
function reportsLastBlock(peers) {
  return peers.some((p) => typeof p.last_block === 'number');
}

/** Whether the per-message counters needed here are present at all. */
function reportsBlockBytes(peers) {
  return peers.some((p) => blockBytes(p) !== null);
}

/** Remember every peer's block bytes. Keyed by the node's connection id, which a reconnect changes. */
function takeBaseline(peers, atMs) {
  const bytes = new Map();
  for (const p of peers) {
    const b = blockBytes(p);
    if (b !== null && p.id !== undefined) bytes.set(p.id, b);
  }
  return { atMs, bytes };
}

/**
 * Ids of the peers whose block bytes grew since the baseline.
 *
 * A peer with no baseline entry either connected after the baseline was taken
 * (its counter started at zero, so all of it is new) or was there and had no
 * readable counter (unknown, so not credited).
 */
function creditedSince(peers, baseline) {
  const credited = new Set();
  if (!baseline) return credited;
  for (const p of peers) {
    const now = blockBytes(p);
    if (now === null || p.id === undefined) continue;
    let before = baseline.bytes.get(p.id);
    if (before === undefined) {
      const connectedAtMs = typeof p.conntime === 'number' ? p.conntime * 1000 : null;
      // One second of slack: conntime has one-second resolution.
      if (connectedAtMs !== null && connectedAtMs >= baseline.atMs - 1000) before = 0;
      else continue;
    }
    if (now - before >= MIN_DELIVERY_BYTES) credited.add(p.id);
  }
  return credited;
}

module.exports = { blockBytes, reportsLastBlock, reportsBlockBytes, takeBaseline, creditedSince, BLOCK_MESSAGES, MIN_DELIVERY_BYTES };
