'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');
const peerMap = require('../src/lib/peer-map');

// A stand-in for the map's own server, on the port the proxy is told to use.
function withUpstream(fn) {
  return async () => {
    const seen = [];
    const upstream = http.createServer((req, res) => {
      seen.push({ method: req.method, url: req.url, cookie: req.headers.cookie, auth: req.headers.authorization });
      res.writeHead(200, { 'Content-Type': 'application/json', 'Set-Cookie': 'x=1', 'Content-Security-Policy': "default-src 'self'" });
      res.end('{"peers":[]}');
    });
    await new Promise((r) => upstream.listen(0, '127.0.0.1', r));
    process.env.PEER_MAP_PORT = String(upstream.address().port);
    const front = http.createServer((req, res) => {
      const url = new URL(req.url, 'http://internal');
      if (!peerMap.proxy(req, res, url)) { res.writeHead(599); res.end('not the map'); }
    });
    await new Promise((r) => front.listen(0, '127.0.0.1', r));
    const ask = (path, method = 'GET') => new Promise((resolve, reject) => {
      const q = http.request({ host: '127.0.0.1', port: front.address().port, path, method, headers: { cookie: 'session=secret', authorization: 'Basic abc' } }, (r) => {
        let body = '';
        r.on('data', (c) => { body += c; });
        r.on('end', () => resolve({ status: r.statusCode, headers: r.headers, body }));
      });
      q.on('error', reject);
      q.end();
    });
    try {
      await fn(ask, seen);
    } finally {
      delete process.env.PEER_MAP_PORT;
      upstream.close();
      front.close();
    }
  };
}

test('the map is served under /map/, and only what its page loads', withUpstream(async (ask, seen) => {
  const peers = await ask('/map/api/peers');
  assert.equal(peers.status, 200);
  assert.equal(peers.body, '{"peers":[]}');
  assert.equal(peers.headers['content-security-policy'], "default-src 'self'");
  assert.equal(peers.headers['set-cookie'], undefined, 'no cookie comes back from the map');
  assert.deepEqual(seen[0], { method: 'GET', url: '/api/peers', cookie: undefined, auth: undefined }, 'no cookie or login goes to the map');

  assert.equal((await ask('/map/')).status, 200);
  assert.equal(seen[1].url, '/');
  for (const path of ['/map/api/health', '/map/api/peers/', '/map/%61pi/peers', '/map//evil.example/x']) {
    assert.equal((await ask(path)).status, 404, path);
  }
  // Resolved before it is looked at: this is /api/peers, the dashboard's own, and not the map's.
  assert.equal((await ask('/map/../api/peers')).status, 599);
  assert.equal(seen.length, 2, 'nothing else reached the map');
}));

test('the map takes no writes, and /map leads to the page', withUpstream(async (ask, seen) => {
  for (const method of ['POST', 'PUT', 'DELETE']) assert.equal((await ask('/map/api/peers', method)).status, 405, method);
  const bare = await ask('/map?peer=1.2.3.4%3A8333');
  assert.equal(bare.status, 308);
  assert.equal(bare.headers.location, 'map/?peer=1.2.3.4%3A8333');
  assert.equal(seen.length, 0);
  assert.equal((await ask('/mapping')).status, 599, 'a path that only starts the same is not the map\'s');
}));

test('the raw peer list is for this container only', () => {
  for (const a of ['127.0.0.1', '::1', '::ffff:127.0.0.1']) assert.equal(peerMap.isLoopback(a), true, a);
  for (const a of ['10.21.0.1', '10.21.21.9', '::ffff:10.21.0.5', '', undefined]) assert.equal(peerMap.isLoopback(a), false, String(a));
});

test('without a binary there is no map to start', () => {
  process.env.PEER_MAP_BIN = '/nonexistent/peer-map';
  const lines = [];
  try {
    assert.equal(peerMap.start({ info: (m) => lines.push(m), warn: () => {} }, 8788), false);
    assert.equal(lines.length, 1);
  } finally {
    delete process.env.PEER_MAP_BIN;
  }
});
