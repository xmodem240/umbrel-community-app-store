'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const net = require('net');
const { legacyToCashAddr, loginForms, decodeLegacy } = require('../src/lib/address-format');
const { StratumPoolConnection } = require('../src/lib/stratum-client');

test('legacy -> CashAddr matches the published CashAddr test vectors', () => {
  assert.equal(legacyToCashAddr('1BpEi6DfDAUFd7GtittLSdBeYJvcoaVggu'), 'bitcoincash:qpm2qsznhks23z7629mms6s4cwef74vcwvy22gdx6a');
  assert.equal(legacyToCashAddr('3CWFddi6m4ndiGyKqzYvsFYagqDLPVMTzC'), 'bitcoincash:ppm2qsznhks23z7629mms6s4cwef74vcwvn0h829pq');
});

test('a bad checksum or junk is not converted', () => {
  assert.equal(legacyToCashAddr('1BpEi6DfDAUFd7GtittLSdBeYJvcoaVgg'), null);
  assert.equal(decodeLegacy('not an address!'), null);
  assert.deepEqual(loginForms('bitcoincash:qpm2qsznhks23z7629mms6s4cwef74vcwvy22gdx6a'), ['bitcoincash:qpm2qsznhks23z7629mms6s4cwef74vcwvy22gdx6a']);
});

test('the default login offers legacy first, then CashAddr', () => {
  const forms = loginForms('1BitcoinEaterAddressDontSendf59kuE');
  assert.equal(forms.length, 2);
  assert.match(forms[1], /^bitcoincash:q/);
});

// A pool that, like 2Miners BCH, refuses the legacy spelling.
function fakePool(accept) {
  const seen = [];
  const server = net.createServer((sock) => {
    let buf = '';
    sock.on('data', (c) => {
      buf += c;
      let i;
      while ((i = buf.indexOf('\n')) >= 0) {
        const msg = JSON.parse(buf.slice(0, i)); buf = buf.slice(i + 1);
        if (msg.method === 'mining.authorize') {
          seen.push(msg.params[0]);
          const ok = accept(msg.params[0]);
          sock.write(`${JSON.stringify({ id: 2, result: ok, error: ok ? null : [20, 'Invalid address', null] })}\n`);
          if (!ok) sock.end();
        }
      }
    });
    sock.on('error', () => {});
  });
  return new Promise((r) => server.listen(0, '127.0.0.1', () => r({ server, seen, port: server.address().port })));
}

test('a rejected legacy login is retried as CashAddr and the retry delay is not reset', async () => {
  const pool = await fakePool((u) => u.startsWith('bitcoincash:'));
  const conn = new StratumPoolConnection({ host: '127.0.0.1', port: pool.port, label: 't', authorizeAddress: '1BitcoinEaterAddressDontSendf59kuE' });
  const results = [];
  conn.on('authorizeResult', (r) => results.push(r));
  conn.start();
  await new Promise((r) => { const t = setInterval(() => { if (results.length >= 1) { clearInterval(t); r(); } }, 20); });
  assert.equal(results[0].ok, false);
  assert.equal(results[0].rejections, 1);
  await new Promise((r) => setTimeout(r, 100));
  assert.ok(conn.reconnectDelayMs > 2000, `delay must grow after a rejection, was ${conn.reconnectDelayMs}`);
  await new Promise((r) => { const t = setInterval(() => { if (results.length >= 2) { clearInterval(t); r(); } }, 50); });
  assert.equal(results[1].ok, true);
  assert.match(pool.seen[0], /^1BitcoinEater/);
  assert.match(pool.seen[1], /^bitcoincash:q/);
  assert.equal(conn.reconnectDelayMs, 2000, 'an accepted login resets the delay');
  conn.stop(); pool.server.close();
});

test('a pool that refuses every spelling is retried ever more slowly', async () => {
  const pool = await fakePool(() => false);
  const conn = new StratumPoolConnection({ host: '127.0.0.1', port: pool.port, label: 't', authorizeAddress: '1BitcoinEaterAddressDontSendf59kuE' });
  conn.rejections = 4; // as if both spellings had already been refused twice
  conn.reconnectDelayMs = 30000;
  assert.equal(conn._nextDelayMs(), 45000, 'the 30 s cap no longer applies once every spelling has been refused');
  conn.rejections = 0;
  assert.equal(conn._nextDelayMs(), 30000);
  pool.server.close();
});
