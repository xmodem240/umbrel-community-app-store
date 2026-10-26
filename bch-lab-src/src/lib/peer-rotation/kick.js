'use strict';

const rpc = require('../rpc');
const db = require('../db');
const logger = require('../logger').make('peer-rotation');
const { logAction } = require('./log');
const { MIN_ELIGIBLE_FOR_JUDGEMENT } = require('./rules');

/**
 * Pass 1: disconnect live, non-trusted outbound peers that have had enough
 * chances in the recent window and never once delivered a block first there.
 *
 * The recent window, not the lifetime record: the same last 500 blocks the
 * dashboard shows. Judged on the lifetime record, a peer that delivered first
 * once, long ago, was spared for good - and the peers that collect such a
 * record are exactly the former manual peers, which BCHN keeps dialling again
 * as ordinary outbound connections. Seen on a real node: three of them held
 * three of BCHN's eight full-relay slots for over a week at 0 of 107, 128 and
 * 238 recent blocks, slowing the search for new candidates. BCHN
 * automatically replaces a dropped outbound connection with a fresh,
 * randomly-selected one - that replacement is the whole mechanism this
 * feature rides on, so kicking dead weight is what actually turns the crank
 * on finding better peers over time, not just cleanup for its own sake.
 *
 * Deliberately scoped to outbound-full-relay only:
 *   - trusted peers are never touched here no matter how they perform -
 *     they were promoted (or added by hand) on purpose.
 *   - block-relay-only peers are BCHN's eclipse protection. BCHN keeps two,
 *     writes them to anchors.dat on a clean shutdown and reconnects to them
 *     first on the next start (net.cpp, MAX_BLOCK_RELAY_ONLY_ANCHORS). Kicking
 *     one throws an anchor away, and BCHN already rotates the temporary
 *     extra block-relay-only peer itself (EvictExtraOutboundPeers).
 *   - inbound peers aren't ours to disconnect-and-replace this way: we
 *     don't control who connects to us, and BCHN does not backfill a
 *     dropped inbound slot with a fresh random peer the way it does for
 *     outbound - dropping one would just lose a connection for nothing.
 *   - feelers/addr-fetch churn too fast to ever reach MIN_ELIGIBLE_FOR_JUDGEMENT
 *     eligible blocks in the first place, so the eligible gate below already
 *     excludes them; the connection_type check is a second, explicit guard.
 */
// A safety catch. "Never first" only means something while the app can tell
// who was first: if that breaks (a clock off, a node that stops reporting),
// every peer reads 0 and would be kicked in turn. So no kick unless most of the
// newest blocks were credited to somebody, and never more than two per pass.
// A block counts when exactly one peer was credited, or several were (byte
// counters: shared); one that was unclear (last_block method, two matches,
// nobody credited) or credited nobody does not.
const ATTRIBUTION_WINDOW = 50;
const ATTRIBUTION_MIN_SHARE = 0.8;
const MAX_KICKS_PER_PASS = 2;

function attributionWorks() {
  const r = db.instance
    .prepare(`SELECT COUNT(*) AS n,
                     COALESCE(SUM(rr.first_count = 1
                              OR (rr.first_count > 1 AND EXISTS (SELECT 1 FROM relay_observation o WHERE o.race_id = rr.id AND o.first = 1))), 0) AS ok
                FROM (SELECT id, first_count FROM relay_race WHERE first_count IS NOT NULL ORDER BY id DESC LIMIT ?) rr`)
    .get(ATTRIBUTION_WINDOW);
  // Too few blocks with a count (a fresh install, old data) to judge either way.
  return r.n < 20 || r.ok / r.n >= ATTRIBUTION_MIN_SHARE;
}

async function kickDeadWeight(ranking) {
  if (!attributionWorks()) {
    logger.warn('rotation: not kicking - most recent blocks were not credited to any peer', { window: ATTRIBUTION_WINDOW });
    return 0;
  }
  const candidates = ranking.filter(
    (p) =>
      p.live &&
      !p.trusted &&
      p.connectionType === 'outbound-full-relay' &&
      p.recentEligible >= MIN_ELIGIBLE_FOR_JUDGEMENT &&
      p.recentFirst === 0,
  ).slice(0, MAX_KICKS_PER_PASS);

  let kicked = 0;
  for (const peer of candidates) {
    try {
      await rpc.disconnectNode(peer.address);
      kicked += 1;
      logAction({
        action: 'kick',
        address: peer.address,
        firstPct: peer.firstPct,
        eligible: peer.recentEligible,
        note: `${peer.connectionType}, 0/${peer.recentEligible} recent blocks first`,
      });
      logger.info('rotation: kicked a dead-weight outbound peer', {
        address: peer.address,
        eligible: peer.eligible,
        connectionType: peer.connectionType,
      });
    } catch (err) {
      logger.warn('rotation: failed to disconnect a dead-weight peer', { address: peer.address, error: err.message });
    }
  }
  return kicked;
}

module.exports = { kickDeadWeight };
