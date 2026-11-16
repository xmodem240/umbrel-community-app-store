'use strict';

/**
 * The peer map: where the node's peers are, who hosts them and what software
 * they run. It is upstream's Peer Map (map/, Go, one static binary), adapted
 * to Bitcoin Cash, and it runs inside the dashboard's own container:
 *
 *   - the dashboard starts it on loopback and starts it again if it stops;
 *   - it gets the node's peers from this dashboard (/api/node/peerinfo), so
 *     the node's address and login stay in one place, the Node card;
 *   - the browser reaches it only through this server, under /map/, GET only.
 *
 * No extra container and no extra port: nothing about how Umbrel starts the
 * app changes. Without the binary (a development checkout) there is simply
 * no map and no link to one.
 */

const fs = require('fs');
const http = require('http');
const path = require('path');
const { spawn } = require('child_process');

const BIN = () => process.env.PEER_MAP_BIN || path.join(__dirname, '..', '..', 'peer-map');
const PORT = () => {
  const p = Number(process.env.PEER_MAP_PORT);
  return Number.isInteger(p) && p > 0 && p < 65536 ? p : 8789;
};
const base = () => `http://127.0.0.1:${PORT()}`;

// Restarts: at once the first time, then slower, so a binary that cannot run
// costs a line in the log every five minutes and no more.
const BACKOFF_MS = [1000, 5000, 30000, 300000];

let child = null;
let stopped = false;
let failures = 0;
let timer = null;

function available() {
  if (process.env.PEER_MAP === 'off') return false;
  try {
    fs.accessSync(BIN(), fs.constants.X_OK);
    return true;
  } catch {
    return false;
  }
}

function run(logger, dashboardPort) {
  if (stopped) return;
  const startedAt = Date.now();
  child = spawn(BIN(), [], {
    stdio: ['ignore', 'pipe', 'pipe'],
    // Only what it needs: none of this process's environment, which holds
    // nothing for it and need not travel.
    env: {
      PATH: process.env.PATH || '',
      PEERMAP_HOST: '127.0.0.1',
      PEERMAP_PORT: String(PORT()),
      PEERMAP_PEERS_URL: `http://127.0.0.1:${dashboardPort}/api/node/peerinfo`,
      PEERMAP_SIBLING_HEALTH_URL: `http://127.0.0.1:${dashboardPort}/api/health`,
    },
  });
  const say = (level) => (chunk) => {
    for (const line of String(chunk).split('\n')) if (line.trim()) logger[level](`peer map: ${line.trim().slice(0, 500)}`);
  };
  child.stdout.on('data', say('info'));
  child.stderr.on('data', say('info'));
  child.on('error', (err) => logger.warn('peer map could not be started', { error: err.message }));
  child.on('exit', (code, signal) => {
    child = null;
    if (stopped) return;
    // Ran for ten minutes: whatever ended it, it was working. Start over.
    if (Date.now() - startedAt > 10 * 60 * 1000) failures = 0;
    const wait = BACKOFF_MS[Math.min(failures, BACKOFF_MS.length - 1)];
    failures += 1;
    logger.warn('peer map stopped, starting it again', { code, signal, inMs: wait });
    timer = setTimeout(() => run(logger, dashboardPort), wait);
    timer.unref?.();
  });
}

/** Start the map beside the dashboard. Returns whether there is one to start. */
function start(logger, dashboardPort) {
  if (!available()) {
    logger.info('peer map not started', { reason: process.env.PEER_MAP === 'off' ? 'switched off (PEER_MAP=off)' : 'no binary in this build' });
    return false;
  }
  stopped = false;
  // The dashboard's own check for the map (lib/sibling.js) and the marks it
  // puts on the ranking read this address.
  if (!process.env.PEERMAP_HEALTH_URL) process.env.PEERMAP_HEALTH_URL = `${base()}/api/health`;
  run(logger, dashboardPort);
  return true;
}

function stop() {
  stopped = true;
  clearTimeout(timer);
  if (child) {
    try { child.kill('SIGTERM'); } catch { /* already gone */ }
  }
}

// Only what the map's page loads, spelled exactly so; everything else is 404.
const PAGE_PATHS = new Map([
  ['/map/', '/'],
  ['/map/app.js', '/app.js'],
  ['/map/style.css', '/style.css'],
  ['/map/world.json', '/world.json'],
  ['/map/icon.svg', '/icon.svg'],
  ['/map/apple-touch-icon.png', '/apple-touch-icon.png'],
  ['/map/api/peers', '/api/peers'],
]);

/**
 * Pass a request under /map/ on to the map. Returns false when the path is
 * not the map's. Read-only: no method but GET and HEAD, no cookies, no
 * credentials and no body go through.
 */
function proxy(req, res, url) {
  if (url.pathname !== '/map' && !url.pathname.startsWith('/map/')) return false;
  const plain = (status, text, extra = {}) => {
    res.writeHead(status, { 'Content-Type': 'text/plain; charset=utf-8', 'Cache-Control': 'no-store', 'X-Content-Type-Options': 'nosniff', ...extra });
    res.end(text);
    return true;
  };
  if (req.method !== 'GET' && req.method !== 'HEAD') return plain(405, 'Method not allowed', { Allow: 'GET, HEAD' });
  // Relative, so it holds behind any prefix; the page's own links are relative too.
  if (url.pathname === '/map') return plain(308, '', { Location: `map/${url.search}` });
  const target = PAGE_PATHS.get(url.pathname);
  if (!target) return plain(404, 'Not found');
  const headers = {};
  for (const name of ['accept', 'accept-encoding', 'if-none-match', 'if-modified-since']) {
    if (req.headers[name]) headers[name] = req.headers[name];
  }
  const request = http.request({ host: '127.0.0.1', port: PORT(), path: target, method: req.method, headers }, (reply) => {
    const outgoing = {};
    for (const name of ['content-type', 'content-length', 'content-encoding', 'cache-control', 'etag', 'last-modified', 'vary',
      'content-security-policy', 'x-content-type-options', 'referrer-policy']) {
      if (reply.headers[name] !== undefined) outgoing[name] = reply.headers[name];
    }
    res.writeHead(reply.statusCode, outgoing);
    reply.on('error', () => res.destroy());
    reply.pipe(res);
  });
  // The map holds its answer while it asks the node: up to a minute.
  const deadline = setTimeout(() => request.destroy(new Error('peer map timeout')), 70_000);
  res.on('close', () => { clearTimeout(deadline); request.destroy(); });
  request.on('error', () => {
    clearTimeout(deadline);
    if (res.headersSent) return res.destroy();
    plain(502, 'The peer map is not running yet. Reload the page in a moment.');
  });
  request.end();
  return true;
}

const isLoopback = (address) => address === '127.0.0.1' || address === '::1' || address === '::ffff:127.0.0.1';

module.exports = { start, stop, proxy, available, isLoopback, base };
