'use strict';

/**
 * Make a BCHN peer look like the peer this app was written for.
 *
 * The app was written against Bitcoin Core's getpeerinfo. BCHN's is older and
 * different, read from its source (rpc/net.cpp, v29.0.0): it has no
 * `connection_type`, no `network`, no `servicesnames` and no `last_block`.
 * What it has instead is `addnode` (a manual connection), `inbound`, the hex
 * `services`, and `bytesrecv_per_msg`.
 *
 * Without this, every outbound peer was typed "unknown" - which also made the
 * rotation's `outbound-full-relay` checks match nothing - and the service
 * names, which decide "offers no services", were never filled in.
 *
 * Only fields that are MISSING are filled in, so a node that does report them
 * (Bitcoin Core, a newer BCHN) is passed through unchanged. `last_block` is
 * not invented: nothing here can know it. See lib/delivery-bytes.js for how
 * First is credited without it.
 */

// BCHN's service bits (protocol.h), named as Bitcoin Core names its own.
const SERVICE_BITS = [
  [0, 'NETWORK'],
  [1, 'GETUTXO'],
  [2, 'BLOOM'],
  [4, 'XTHIN'],
  [5, 'BITCOIN_CASH'],
  [6, 'GRAPHENE'],
  [8, 'CF'],
  [10, 'NETWORK_LIMITED'],
  [11, 'EXTVERSION'],
];

/** "0000000000000425" -> ['NETWORK', 'BLOOM', 'BITCOIN_CASH', ...]; null if it is not a hex string. */
function serviceNames(hex) {
  if (typeof hex !== 'string' || !/^[0-9a-fA-F]{1,16}$/.test(hex)) return null;
  const value = BigInt(`0x${hex}`);
  const names = [];
  let known = 0n;
  for (const [bit, name] of SERVICE_BITS) {
    const mask = 1n << BigInt(bit);
    known |= mask;
    if (value & mask) names.push(name);
  }
  // Same spelling Core uses for a bit it has no name for.
  for (let bit = 0; bit < 64; bit += 1) {
    const mask = 1n << BigInt(bit);
    if ((value & mask) && !(known & mask)) names.push(`UNKNOWN[2^${bit}]`);
  }
  return names;
}

/** ipv4 | ipv6 | onion | i2p, from "host:port" or "[v6]:port". */
function networkOf(addr) {
  if (typeof addr !== 'string' || !addr) return null;
  let host = addr;
  if (addr.startsWith('[')) {
    const end = addr.indexOf(']');
    host = end > 0 ? addr.slice(1, end) : addr;
  } else if ((addr.match(/:/g) || []).length === 1) {
    host = addr.slice(0, addr.lastIndexOf(':'));
  }
  if (/\.onion$/i.test(host)) return 'onion';
  if (/\.i2p$/i.test(host)) return 'i2p';
  if (host.includes(':')) return 'ipv6';
  return 'ipv4';
}

/**
 * Core's names for the connection kinds. BCHN does not distinguish
 * block-relay-only, feeler or addr-fetch connections, so an outbound one that
 * was not added by hand is a full-relay one.
 */
function connectionTypeOf(peer) {
  if (peer.addnode === true) return 'manual';
  return peer.inbound ? 'inbound' : 'outbound-full-relay';
}

function normalisePeer(peer) {
  if (!peer || typeof peer !== 'object') return peer;
  const out = { ...peer };
  if (out.connection_type === undefined) out.connection_type = connectionTypeOf(out);
  if (out.network === undefined) {
    const network = networkOf(out.addr);
    if (network) out.network = network;
  }
  if (out.servicesnames === undefined) {
    const names = serviceNames(out.services);
    if (names) out.servicesnames = names;
  }
  return out;
}

const normalisePeers = (peers) => (Array.isArray(peers) ? peers.map(normalisePeer) : peers);

module.exports = { normalisePeer, normalisePeers, serviceNames, networkOf, connectionTypeOf };
