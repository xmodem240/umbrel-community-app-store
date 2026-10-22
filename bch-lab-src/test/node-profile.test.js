'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const os = require('os');
const path = require('path');
const http = require('http');
const net = require('net');

const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'bchlab-node-profile-'));
process.env.DATA_DIR = tmpDir;
process.env.LOG_LEVEL = 'error';
for (const k of Object.keys(process.env)) {
  if (/^(APP_)?BITCOIN_/.test(k)) delete process.env[k];
}

const nodeProfile = require('../src/lib/node-profile');

const file = () => path.join(tmpDir, 'node-profile.json');
const clean = () => { try { fs.unlinkSync(file()); } catch { /* none */ } };

test('with no saved file the environment decides, and BCHN defaults fill the gaps', () => {
  clean();
  const r = nodeProfile.resolve();
  assert.equal(r.source, 'environment');
  assert.equal(r.rpcPort, 8332);
  assert.equal(r.zmqPort, 28332);
  assert.equal(r.host, '127.0.0.1');
  assert.equal(r.profile, 'custom');

  process.env.BITCOIN_RPC_HOST = 'sslabs-bitcoin-cash-node_bitcoind_1';
  process.env.BITCOIN_RPC_USER = 'bchn';
  assert.equal(nodeProfile.resolve().profile, 'lonestrike', 'the Umbrel default is recognised by its host');
  delete process.env.BITCOIN_RPC_HOST;
  delete process.env.BITCOIN_RPC_USER;
});

test('a saved file beats the environment', () => {
  clean();
  process.env.BITCOIN_RPC_HOST = 'from-env';
  const s = nodeProfile.normalise({ profile: 'custom', host: 'from-file', rpcUser: 'u', rpcPass: 'p', rpcPort: 9000, zmqPort: 9001 });
  nodeProfile.save(s);
  const r = nodeProfile.resolve();
  assert.equal(r.source, 'file');
  assert.equal(r.host, 'from-file');
  assert.equal(r.rpcPort, 9000);
  assert.equal(r.zmqHost, 'from-file', 'ZMQ host defaults to the RPC host');
  assert.equal(r.zmqPort, 9001);
  delete process.env.BITCOIN_RPC_HOST;
  clean();
});

test('the LoneStrike profile needs nothing typed and its login is built in', () => {
  const s = nodeProfile.normalise({ profile: 'lonestrike' });
  assert.equal(s.host, 'sslabs-bitcoin-cash-node_bitcoind_1');
  assert.equal(s.rpcUser, 'bchn');
  assert.ok(s.rpcPass);
  assert.equal(s.zmqPort, 28332);
  // And what the browser is told never includes it.
  const listed = nodeProfile.listProfiles().find((p) => p.id === 'lonestrike');
  assert.equal(JSON.stringify(listed).includes(s.rpcPass), false);
});

test('AxeBCH needs only the password: host, ports and user come from its own compose file', () => {
  assert.throws(() => nodeProfile.normalise({ profile: 'axebch' }), /RPC password is required/);
  const ok = nodeProfile.normalise({ profile: 'axebch', rpcPass: 'per-install-password' });
  assert.equal(ok.host, 'willitmod-dev-bch_bchn_1');
  assert.equal(ok.rpcPort, 28332);
  assert.equal(ok.rpcUser, 'bch');
  assert.equal(ok.zmqPort, 28334);
  assert.equal(ok.zmqHost, 'willitmod-dev-bch_bchn_1');
  assert.equal(ok.rpcPass, 'per-install-password');
  assert.equal(nodeProfile.listProfiles().find((p) => p.id === 'axebch').defaults.rpcUser, 'bch');
});

test('a custom node refuses to save without host and login', () => {
  assert.throws(() => nodeProfile.normalise({ profile: 'custom', host: 'x_y_1' }), /RPC user is required/);
  assert.throws(() => nodeProfile.normalise({ profile: 'custom', host: 'x_y_1', rpcUser: 'u' }), /RPC password is required/);
  assert.throws(() => nodeProfile.normalise({ profile: 'custom', rpcUser: 'u', rpcPass: 'p' }), /Host is required/);
});

test('bad input is rejected with a readable message', () => {
  const base = { profile: 'custom', host: 'node', rpcUser: 'u', rpcPass: 'p' };
  assert.throws(() => nodeProfile.normalise({ ...base, profile: 'nope' }), /unknown node software/);
  assert.throws(() => nodeProfile.normalise({ ...base, host: 'http://evil/x' }), /Host must be/);
  assert.throws(() => nodeProfile.normalise({ ...base, host: 'a b' }), /Host must be/);
  assert.throws(() => nodeProfile.normalise({ ...base, rpcPort: 70000 }), /RPC port/);
  assert.throws(() => nodeProfile.normalise({ ...base, rpcPort: 'abc' }), /RPC port/);
  assert.throws(() => nodeProfile.normalise({ ...base, zmqPort: 0 }), /ZMQ port/);
  assert.throws(() => nodeProfile.normalise({ ...base, rpcUser: 'a:b' }), /RPC user cannot contain/);
  assert.throws(() => nodeProfile.normalise({ ...base, rpcPass: 'line\nbreak' }), /not allowed/);
  assert.throws(() => nodeProfile.normalise({ ...base, rpcPass: 'x'.repeat(300) }), /too long/);
  assert.throws(() => nodeProfile.normalise(null), /object/);
  assert.equal(nodeProfile.normalise({ ...base, host: '10.21.0.5' }).host, '10.21.0.5');
  assert.equal(nodeProfile.normalise({ ...base, host: '[::1]'.slice(1, -1) }).host, '::1');
});

test('a blank password keeps the saved one only while the host and port are unchanged', () => {
  const current = { host: 'node-a', rpcPort: 8332, rpcPass: 'saved-secret' };
  const same = nodeProfile.normalise({ profile: 'custom', host: 'node-a', rpcUser: 'u', rpcPass: '' }, current);
  assert.equal(same.rpcPass, 'saved-secret');
  assert.throws(
    () => nodeProfile.normalise({ profile: 'custom', host: 'someone-else', rpcUser: 'u', rpcPass: '' }, current),
    /RPC password is required/,
    'the saved password must not follow the form to a different host',
  );
  assert.throws(
    () => nodeProfile.normalise({ profile: 'custom', host: 'node-a', rpcPort: 9999, rpcUser: 'u', rpcPass: '' }, current),
    /RPC password is required/,
  );
});

test('save writes a private file atomically and describe() never includes the password', () => {
  clean();
  const s = nodeProfile.normalise({ profile: 'custom', host: 'node', rpcUser: 'u', rpcPass: 'hunter2' });
  nodeProfile.save(s);
  assert.equal(fs.statSync(file()).mode & 0o777, 0o600);
  assert.deepEqual(fs.readdirSync(tmpDir).filter((f) => f.endsWith('.tmp')), [], 'no temp file left behind');
  const shown = nodeProfile.describe(nodeProfile.resolve());
  assert.equal(shown.hasPassword, true);
  assert.equal(JSON.stringify(shown).includes('hunter2'), false);
  assert.equal('rpcPass' in shown, false);
  clean();
});

test('a corrupt saved file falls back to the environment instead of crashing', () => {
  fs.writeFileSync(file(), '{ not json');
  assert.equal(nodeProfile.resolve().source, 'environment');
  clean();
});

test('signature changes when the file does, which is what restarts the processes', () => {
  clean();
  assert.equal(nodeProfile.signature(), '');
  nodeProfile.save(nodeProfile.normalise({ profile: 'custom', host: 'a', rpcUser: 'u', rpcPass: 'p' }));
  const first = nodeProfile.signature();
  assert.notEqual(first, '');
  fs.writeFileSync(file(), JSON.stringify({ profile: 'custom', host: 'bbbbbbbb', rpcUser: 'u', rpcPass: 'p' }));
  assert.notEqual(nodeProfile.signature(), first);
  clean();
});

// A real HTTP RPC server and a real TCP listener, so the actual request code runs.
function fakeNode({ user = 'u', pass = 'p' } = {}) {
  const rpc = http.createServer((req, res) => {
    let body = '';
    req.on('data', (c) => { body += c; });
    req.on('end', () => {
      const want = `Basic ${Buffer.from(`${user}:${pass}`).toString('base64')}`;
      if (req.headers.authorization !== want) { res.writeHead(401); return res.end(); }
      const { method } = JSON.parse(body);
      const result = method === 'getnetworkinfo'
        ? { subversion: '/Bitcoin Cash Node:29.0.0(EB32.0)/', connections: 12 }
        : { chain: 'main', blocks: 123456, initialblockdownload: false };
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ result, error: null, id: 'x' }));
    });
  });
  const zmq = net.createServer((s) => s.destroy());
  return new Promise((resolve) => {
    rpc.listen(0, '127.0.0.1', () => zmq.listen(0, '127.0.0.1', () => resolve({
      rpcPort: rpc.address().port,
      zmqPort: zmq.address().port,
      close: () => { rpc.close(); zmq.close(); },
    })));
  });
}

test('check() reports a healthy node: software, block height and ZMQ', async () => {
  const n = await fakeNode();
  try {
    const r = await nodeProfile.check({ host: '127.0.0.1', rpcPort: n.rpcPort, rpcUser: 'u', rpcPass: 'p', zmqHost: '127.0.0.1', zmqPort: n.zmqPort });
    assert.equal(r.ok, true);
    assert.match(r.rpc.subversion, /Bitcoin Cash Node/);
    assert.equal(r.rpc.blocks, 123456);
    assert.equal(r.rpc.chain, 'main');
    assert.equal(r.zmq.ok, true);
  } finally { n.close(); }
});

test('check() says what is wrong: bad password, wrong port, missing ZMQ, unknown host', async () => {
  const n = await fakeNode();
  try {
    const base = { host: '127.0.0.1', rpcPort: n.rpcPort, rpcUser: 'u', rpcPass: 'p', zmqHost: '127.0.0.1', zmqPort: n.zmqPort };

    const badPass = await nodeProfile.check({ ...base, rpcPass: 'wrong' });
    assert.equal(badPass.rpc.ok, false);
    assert.match(badPass.rpc.error, /refused the RPC user or password/);
    assert.equal(badPass.zmq.ok, true, 'ZMQ is reported independently of RPC');

    const noZmq = await nodeProfile.check({ ...base, zmqPort: 1 });
    assert.equal(noZmq.rpc.ok, true);
    assert.equal(noZmq.zmq.ok, false);
    assert.match(noZmq.zmq.error, /zmqpubhashblock/);
    assert.equal(noZmq.ok, false);

    const refused = await nodeProfile.check({ ...base, rpcPort: 1 });
    assert.match(refused.rpc.error, /refused the connection/);

    const unknown = await nodeProfile.check({ ...base, host: 'no-such-container-xyz.invalid' });
    assert.match(unknown.rpc.error, /no container or host called/);
  } finally { n.close(); }
});

test('detect() reports only the node apps whose container name resolves', async () => {
  nodeProfile._setLookup(async (host) => {
    if (host === 'sslabs-bitcoin-cash-node_bitcoind_1' || host === 'willitmod-dev-bch_bchn_1') return { address: '10.21.0.3', family: 4 };
    throw Object.assign(new Error('nf'), { code: 'ENOTFOUND' });
  });
  const found = await nodeProfile.detect();
  assert.deepEqual(found.map((f) => [f.profile, f.host]), [
    ['lonestrike', 'sslabs-bitcoin-cash-node_bitcoind_1'],
    ['axebch', 'willitmod-dev-bch_bchn_1'],
  ]);
  assert.equal(found[0].verified, true);
  assert.equal(found[1].verified, true);
  nodeProfile._setLookup(async () => { throw new Error('nf'); });
  assert.deepEqual(await nodeProfile.detect(), []);
  nodeProfile._setLookup(null);
});

test('watch() is off when NODE_PROFILE_WATCH=0 and never keeps the process alive', () => {
  process.env.NODE_PROFILE_WATCH = '0';
  assert.equal(nodeProfile.watch({ info() {} }, ''), null);
  delete process.env.NODE_PROFILE_WATCH;
  const t = nodeProfile.watch({ info() {} }, nodeProfile.signature());
  assert.ok(t.hasRef() === false, 'timer is unref\'d');
  clearInterval(t);
});
