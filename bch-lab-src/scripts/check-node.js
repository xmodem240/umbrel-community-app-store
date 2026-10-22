#!/usr/bin/env node
'use strict';

/**
 * Checks that a Bitcoin Cash Node answers the way Bitcoin Cash Lab expects.
 *
 * Bitcoin Cash Lab was adapted from a Bitcoin Core app; the RPC fields below
 * are the ones it reads. Run this against your node before trusting the
 * rankings. Read-only: it calls getblockchaininfo, getpeerinfo, getnettotals,
 * getblocktemplate and nothing else.
 *
 *   BITCOIN_RPC_HOST=... BITCOIN_RPC_PORT=8332 BITCOIN_RPC_USER=... \
 *   BITCOIN_RPC_PASS=... node scripts/check-node.js
 *
 * Exit code 0 = everything the app needs was present, 1 = something is missing.
 */

const host = process.env.BITCOIN_RPC_HOST || '127.0.0.1';
const port = process.env.BITCOIN_RPC_PORT || '8332';
const user = process.env.BITCOIN_RPC_USER || '';
const pass = process.env.BITCOIN_RPC_PASS || '';

async function call(method, params = []) {
  const res = await fetch(`http://${host}:${port}/`, {
    method: 'POST',
    headers: {
      'content-type': 'application/json',
      authorization: `Basic ${Buffer.from(`${user}:${pass}`).toString('base64')}`,
    },
    body: JSON.stringify({ jsonrpc: '1.0', id: 'check', method, params }),
    signal: AbortSignal.timeout(30000),
  });
  const body = await res.json().catch(() => ({}));
  if (body.error) throw new Error(`${method}: ${body.error.message}`);
  if (!res.ok) throw new Error(`${method}: HTTP ${res.status}`);
  return body.result;
}

let failed = 0;
const ok = (msg) => console.log(`  ok    ${msg}`);
const bad = (msg) => { failed += 1; console.log(`  FAIL  ${msg}`); };
const warn = (msg) => console.log(`  warn  ${msg}`);

(async () => {
  console.log(`Checking ${host}:${port}`);

  const info = await call('getblockchaininfo');
  ok(`getblockchaininfo: chain "${info.chain}", ${info.blocks} blocks, initialblockdownload=${info.initialblockdownload}`);
  console.log('        (the app\'s BITCOIN_NETWORK should map to this chain name for default ports)');

  const peers = await call('getpeerinfo');
  ok(`getpeerinfo: ${peers.length} peers`);
  if (peers.length === 0) {
    warn('no peers connected - field checks below are skipped; run again once connected');
  } else {
    // Fields the app reads. 'critical' ones break the ranking if absent.
    const critical = ['addr', 'last_block', 'inbound', 'conntime'];
    const optional = ['connection_type', 'network', 'bip152_hb_to', 'subver', 'servicesnames', 'relaytxes', 'minping'];
    for (const f of critical) {
      const n = peers.filter((p) => p[f] !== undefined).length;
      if (n === peers.length) ok(`peer field "${f}" present on all peers`);
      else bad(`peer field "${f}" missing on ${peers.length - n} of ${peers.length} peers - the First ranking depends on it`);
    }
    for (const f of optional) {
      const n = peers.filter((p) => p[f] !== undefined).length;
      if (n === peers.length) ok(`peer field "${f}" present`);
      else warn(`peer field "${f}" missing on ${peers.length - n} of ${peers.length} peers - some features degrade`);
    }
    const types = [...new Set(peers.map((p) => p.connection_type).filter(Boolean))];
    if (types.length) console.log(`        connection_type values seen: ${types.join(', ')}`);
    const withBlock = peers.filter((p) => p.last_block > 0).length;
    console.log(`        peers that have delivered a block (last_block > 0): ${withBlock}`);
  }

  await call('getnettotals').then(() => ok('getnettotals'), (e) => bad(e.message));
  await call('getaddednodeinfo').then(() => ok('getaddednodeinfo'), (e) => bad(e.message));

  // The template-timing feature asks for a plain template. Some BCHN versions
  // refuse until synced or when the node has no peers; that is not fatal.
  await call('getblocktemplate', [{}]).then(
    (t) => ok(`getblocktemplate with {}: ${t.transactions ? t.transactions.length : '?'} transactions`),
    (e) => warn(`getblocktemplate: ${e.message} (template timing will log a warning and carry on)`),
  );

  console.log(failed ? `\n${failed} required check(s) failed.` : '\nAll required checks passed.');
  process.exit(failed ? 1 : 0);
})().catch((err) => {
  console.error(`Could not talk to the node: ${err.message}`);
  process.exit(1);
});
