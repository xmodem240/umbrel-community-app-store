'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { normalisePeer, serviceNames, connectionTypeOf } = require('../src/lib/peer-compat');
const d = require('../src/lib/delivery-bytes');

const bchnPeer = (o = {}) => ({
  id: 1, addr: '1.2.3.4:8333', inbound: false, addnode: false, conntime: 1000,
  services: '0000000000000025', bytesrecv_per_msg: { inv: 500 }, ...o,
});

test('normalisePeer fills what BCHN omits', () => {
  const p = normalisePeer(bchnPeer());
  assert.equal(p.connection_type, 'outbound-full-relay');
  assert.equal(p.network, 'ipv4');
  assert.deepEqual(p.servicesnames, ['NETWORK', 'BLOOM', 'BITCOIN_CASH']);
});

test('connection type: addnode wins, inbound otherwise', () => {
  assert.equal(connectionTypeOf({ addnode: true, inbound: true }), 'manual');
  assert.equal(connectionTypeOf({ inbound: true }), 'inbound');
});

test('normalisePeer keeps fields the node did report', () => {
  const p = normalisePeer(bchnPeer({ connection_type: 'block-relay-only', network: 'onion' }));
  assert.equal(p.connection_type, 'block-relay-only');
  assert.equal(p.network, 'onion');
});

test('serviceNames returns null (unknown, not empty) for junk', () => {
  assert.equal(serviceNames('zz'), null);
});

test('mode detection', () => {
  assert.equal(d.reportsLastBlock([bchnPeer()]), false);
  assert.equal(d.reportsLastBlock([bchnPeer({ last_block: 0 })]), true);
  assert.equal(d.reportsBlockBytes([bchnPeer()]), true);
  assert.equal(d.reportsBlockBytes([{ id: 1 }]), false);
});

test('credit only peers whose block bytes grew since the baseline', () => {
  const before = [
    bchnPeer({ id: 1, bytesrecv_per_msg: { cmpctblock: 1000 } }),
    bchnPeer({ id: 2, bytesrecv_per_msg: { cmpctblock: 1000 } }),
    bchnPeer({ id: 3, bytesrecv_per_msg: { inv: 10 } }),
  ];
  const base = d.takeBaseline(before, 50000);
  const after = [
    bchnPeer({ id: 1, bytesrecv_per_msg: { cmpctblock: 1000 } }),
    bchnPeer({ id: 2, bytesrecv_per_msg: { cmpctblock: 21000 } }),
    bchnPeer({ id: 3, bytesrecv_per_msg: { inv: 9999 } }), // tx/inv traffic is not a block
  ];
  assert.deepEqual([...d.creditedSince(after, base)], [2]);
});

test('block and cmpctblock both count; ties allowed', () => {
  const base = d.takeBaseline([bchnPeer({ id: 1 }), bchnPeer({ id: 2 })], 50000);
  const after = [
    bchnPeer({ id: 1, bytesrecv_per_msg: { block: 900000 } }),
    bchnPeer({ id: 2, bytesrecv_per_msg: { cmpctblock: 12000 } }),
  ];
  assert.deepEqual([...d.creditedSince(after, base)].sort(), [1, 2]);
});

test('a tiny counter move is noise, not delivery', () => {
  const base = d.takeBaseline([bchnPeer({ id: 1, bytesrecv_per_msg: { cmpctblock: 100 } })], 50000);
  const after = [bchnPeer({ id: 1, bytesrecv_per_msg: { cmpctblock: 120 } })];
  assert.equal(d.creditedSince(after, base).size, 0);
});

test('a peer connected after the baseline is credited from zero; an older unknown one is not', () => {
  const base = d.takeBaseline([bchnPeer({ id: 1 })], 100000);
  const after = [
    bchnPeer({ id: 1 }),
    bchnPeer({ id: 9, conntime: 100, bytesrecv_per_msg: { cmpctblock: 5000 } }),  // connected at 100s, after baseline
    bchnPeer({ id: 8, conntime: 10, bytesrecv_per_msg: { cmpctblock: 5000 } }),   // existed before, unknown to baseline
  ];
  assert.deepEqual([...d.creditedSince(after, base)], [9]);
});
