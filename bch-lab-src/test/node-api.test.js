'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const os = require('os');
const path = require('path');
const http = require('http');
const net = require('net');

const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'bchlab-node-api-'));
process.env.SQLITE_PATH = path.join(tmpDir, 'test.db');
process.env.DATA_DIR = tmpDir;
process.env.LOG_LEVEL = 'error';
process.env.PEERMAP_HEALTH_URL = 'off';
process.env.NODE_PROFILE_WATCH = '0';

const SECRET = 'rpc-secret-do-not-leak';
const rpc = http.createServer((req, res) => {
  let body = '';
  req.on('data', (c) => { body += c; });
  req.on('end', () => {
    const want = `Basic ${Buffer.from(`bchn:${SECRET}`).toString('base64')}`;
    if (req.headers.authorization !== want) { res.writeHead(401); return res.end(); }
    const { method } = JSON.parse(body);
    const result = method === 'getnetworkinfo'
      ? { subversion: '/Bitcoin Cash Node:29.0.0/', connections: 8 }
      : { chain: 'main', blocks: 42, initialblockdownload: false };
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ result, error: null }));
  });
});
const zmq = net.createServer((s) => s.destroy());

let baseUrl;
let server;
let db;

test.before(async () => {
  await new Promise((r) => rpc.listen(0, '127.0.0.1', r));
  await new Promise((r) => zmq.listen(0, '127.0.0.1', r));
  // The environment points at the fake node before config is first loaded.
  process.env.BITCOIN_RPC_HOST = '127.0.0.1';
  process.env.BITCOIN_RPC_PORT = String(rpc.address().port);
  process.env.BITCOIN_RPC_USER = 'bchn';
  process.env.BITCOIN_RPC_PASS = SECRET;
  process.env.BITCOIN_ZMQ_HASHBLOCK_PORT = String(zmq.address().port);
  db = require('../src/lib/db');
  db.open();
  ({ server } = require('../src/dashboard-server'));
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  baseUrl = `http://127.0.0.1:${server.address().port}`;
});

test.after(() => { server.close(); rpc.close(); zmq.close(); });

async function api(pathname, options = {}) {
  const res = await fetch(`${baseUrl}${pathname}`, {
    ...options,
    headers: { 'Content-Type': 'application/json', ...(options.headers || {}) },
  });
  const raw = await res.text();
  let body = null;
  try { body = JSON.parse(raw); } catch { /* not json */ }
  return { status: res.status, body, raw };
}

test('GET /api/node shows the active node and a live connection check, but never the password', async () => {
  const { status, body, raw } = await api('/api/node');
  assert.equal(status, 200);
  assert.equal(body.active.source, 'environment');
  assert.equal(body.active.hasPassword, true);
  assert.equal(body.connection.ok, true);
  assert.match(body.connection.rpc.subversion, /Bitcoin Cash Node/);
  assert.equal(body.connection.rpc.blocks, 42);
  assert.deepEqual(body.profiles.map((p) => p.id), ['lonestrike', 'axebch', 'custom']);
  assert.equal(raw.includes(SECRET), false, 'the RPC password must not appear anywhere in the reply');
  assert.equal(raw.includes('bchn_local_rpc_pw'), false, 'nor the built-in LoneStrike login');
});

test('POST /api/node/test checks a candidate without saving it', async () => {
  const good = await api('/api/node/test', {
    method: 'POST',
    body: JSON.stringify({ profile: 'custom', host: '127.0.0.1', rpcPort: rpc.address().port, rpcUser: 'bchn', rpcPass: SECRET, zmqPort: zmq.address().port }),
  });
  assert.equal(good.status, 200);
  assert.equal(good.body.ok, true);

  const bad = await api('/api/node/test', {
    method: 'POST',
    body: JSON.stringify({ profile: 'custom', host: '127.0.0.1', rpcPort: rpc.address().port, rpcUser: 'bchn', rpcPass: 'nope', zmqPort: zmq.address().port }),
  });
  assert.equal(bad.status, 200);
  assert.equal(bad.body.rpc.ok, false);
  assert.match(bad.body.rpc.error, /user or password/);
  assert.equal(fs.existsSync(path.join(tmpDir, 'node-profile.json')), false, 'testing must not save anything');
});

test('a blank password in a test reuses the saved one for the same host, and only for it', async () => {
  const same = await api('/api/node/test', {
    method: 'POST',
    body: JSON.stringify({ profile: 'custom', host: '127.0.0.1', rpcPort: rpc.address().port, rpcUser: 'bchn', rpcPass: '', zmqPort: zmq.address().port }),
  });
  assert.equal(same.body.ok, true, 'same host and port: the saved password is used');

  const other = await api('/api/node/test', {
    method: 'POST',
    body: JSON.stringify({ profile: 'custom', host: 'localhost', rpcPort: rpc.address().port, rpcUser: 'bchn', rpcPass: '', zmqPort: zmq.address().port }),
  });
  assert.equal(other.status, 422);
  assert.match(other.body.error, /RPC password is required/);
});

test('POST /api/node validates, then saves and says the app is restarting', async () => {
  const invalid = await api('/api/node', { method: 'POST', body: JSON.stringify({ profile: 'axebch' }) });
  assert.equal(invalid.status, 422, 'AxeBCH without its password is refused');
  assert.equal(fs.existsSync(path.join(tmpDir, 'node-profile.json')), false);

  const saved = await api('/api/node', {
    method: 'POST',
    body: JSON.stringify({ profile: 'axebch', rpcPass: 'axe-secret' }),
  });
  assert.equal(saved.status, 200);
  assert.equal(saved.body.restarting, true);
  assert.equal(saved.raw.includes('axe-secret'), false);

  const onDisk = JSON.parse(fs.readFileSync(path.join(tmpDir, 'node-profile.json'), 'utf8'));
  assert.equal(onDisk.profile, 'axebch');
  assert.equal(onDisk.host, 'willitmod-dev-bch_bchn_1');
  assert.equal(onDisk.rpcPass, 'axe-secret');
  assert.equal(onDisk.rpcPort, 28332);
  assert.equal(onDisk.rpcUser, 'bch');
  assert.equal(onDisk.zmqPort, 28334);

  // The dashboard now reports the saved choice, while still showing what this
  // process started with (it restarts shortly after, in a real deployment).
  const after = await api('/api/node');
  assert.equal(after.body.active.source, 'file');
  assert.equal(after.body.active.profile, 'axebch');
  assert.equal(after.body.appliedProfile, 'custom');
  assert.equal(after.raw.includes('axe-secret'), false);
});

test('writes are refused from other sites and without a JSON content type', async () => {
  const xsite = await api('/api/node/test', {
    method: 'POST',
    headers: { 'Sec-Fetch-Site': 'cross-site' },
    body: JSON.stringify({ profile: 'lonestrike' }),
  });
  assert.equal(xsite.status, 403);

  const form = await fetch(`${baseUrl}/api/node`, { method: 'POST', headers: { 'Content-Type': 'text/plain' }, body: '{}' });
  assert.equal(form.status, 415);
});

test('GET /api/node/detect answers with a list', async () => {
  const { status, body } = await api('/api/node/detect');
  assert.equal(status, 200);
  assert.ok(Array.isArray(body.found));
});
