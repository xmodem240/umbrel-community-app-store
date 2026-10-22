'use strict';

/**
 * Which Bitcoin Cash node software this app reads from, chosen at runtime.
 *
 * Umbrel gives an app no settings screen, and the Bitcoin Cash node apps do not
 * export their RPC details to the apps that depend on them, so the choice cannot
 * be made in the compose file for everyone. It is made on the dashboard instead
 * and saved in DATA_DIR/node-profile.json, which all four processes share.
 *
 * Resolution order, for every field:
 *   1. the saved file, if there is one;
 *   2. the BITCOIN_* / APP_BITCOIN_* environment (what the compose file sets,
 *      and what local development uses);
 *   3. BCHN's own defaults.
 *
 * The four processes read this once, at start. To apply a change they restart:
 * each one polls the file's signature (watch) and exits non-zero when it moves,
 * and Docker's restart policy (on-failure) brings it back reading the new
 * file. That is deliberately dull. Re-pointing live RPC and ZMQ connections in
 * four processes without losing a block would be a lot of machinery for
 * something done once.
 *
 * The password is kept in the file in the clear (RPC needs it in the clear), mode
 * 0600, inside the app's own data directory. The dashboard never sends it back
 * to the browser.
 */

const fs = require('fs');
const path = require('path');
const net = require('net');
const http = require('http');
const dns = require('dns');

const FILE_NAME = 'node-profile.json';
const WATCH_INTERVAL_MS = 5000;
const MAX_FIELD = 256;

const pick = (...values) => {
  for (const v of values) if (v !== undefined && v !== null && v !== '') return v;
  return undefined;
};

/**
 * The node apps this knows about. `verified` says whether the connection
 * details were read from that app's own files (true) or are a best guess the
 * operator must confirm with Test (false). The dashboard shows that plainly.
 */
const PROFILES = {
  lonestrike: {
    id: 'lonestrike',
    label: 'Bitcoin Cash Node (LoneStrike Labs app)',
    software: 'BCHN',
    verified: true,
    fixedLogin: true,
    note: 'Umbrel app "Bitcoin Cash Node" (sslabs-bitcoin-cash-node). Its RPC login and ZMQ port are fixed in that app, so there is nothing to type.',
    defaults: {
      host: 'sslabs-bitcoin-cash-node_bitcoind_1',
      rpcPort: 8332,
      rpcUser: 'bchn',
      rpcPass: 'bchn_local_rpc_pw_2f9c',
      zmqHost: '',
      zmqPort: 28332,
    },
    // Container names that mean "this app is installed" (see detect()).
    candidates: ['sslabs-bitcoin-cash-node_bitcoind_1'],
  },
  axebch: {
    id: 'axebch',
    label: 'AxeBCH (WillItMod)',
    software: 'BCHN',
    verified: true,
    fixedLogin: false,
    note:
      'Node + solo pool app from the WillItMod store (willitmod-dev-bch). Host, ports and user are read from its own compose file ' +
      '(v0.9.27). The RPC password is generated for each install, so you have to enter it: on your Umbrel run ' +
      '"grep rpcpassword ~/umbrel/app-data/willitmod-dev-bch/data/node/bitcoin.conf".',
    defaults: {
      host: 'willitmod-dev-bch_bchn_1',
      rpcPort: 28332,
      rpcUser: 'bch',
      rpcPass: '',
      zmqHost: '',
      zmqPort: 28334,
    },
    candidates: ['willitmod-dev-bch_bchn_1'],
  },
  custom: {
    id: 'custom',
    label: 'Other BCHN-compatible node',
    software: 'BCHN-compatible',
    verified: false,
    fixedLogin: false,
    note: 'Any node that speaks Bitcoin Core-style JSON-RPC and publishes ZMQ hashblock (BCHN, Bitcoin ABC, BCHD with an RPC shim, a node on another machine).',
    defaults: { host: '', rpcPort: 8332, rpcUser: '', rpcPass: '', zmqHost: '', zmqPort: 28332 },
    candidates: [],
  },
};

function dataDir() {
  return pick(process.env.DATA_DIR, '/data');
}

function filePath() {
  return path.join(dataDir(), FILE_NAME);
}

/** "mtimeMs:size" of the saved file, or '' when there is none. */
function signature() {
  try {
    const st = fs.statSync(filePath());
    return `${st.mtimeMs}:${st.size}`;
  } catch {
    return '';
  }
}

function readFile() {
  try {
    const parsed = JSON.parse(fs.readFileSync(filePath(), 'utf8'));
    return parsed && typeof parsed === 'object' ? parsed : null;
  } catch {
    return null;
  }
}

function fromEnvironment() {
  const host = pick(process.env.BITCOIN_RPC_HOST, process.env.APP_BITCOIN_NODE_IP, '127.0.0.1');
  return {
    host,
    rpcPort: Number(pick(process.env.BITCOIN_RPC_PORT, process.env.APP_BITCOIN_RPC_PORT, '8332')),
    rpcUser: pick(process.env.BITCOIN_RPC_USER, process.env.APP_BITCOIN_RPC_USER, ''),
    rpcPass: pick(process.env.BITCOIN_RPC_PASS, process.env.APP_BITCOIN_RPC_PASS, ''),
    zmqHost: pick(process.env.BITCOIN_ZMQ_HOST, process.env.APP_BITCOIN_NODE_IP, host),
    zmqPort: Number(pick(process.env.BITCOIN_ZMQ_HASHBLOCK_PORT, process.env.APP_BITCOIN_ZMQ_HASHBLOCK_PORT, '28332')),
  };
}

/** Name of the profile whose host the environment happens to point at. */
function profileForHost(host) {
  for (const p of Object.values(PROFILES)) {
    if (p.defaults.host && p.defaults.host === host) return p.id;
  }
  return 'custom';
}

/**
 * The settings in force, with the password. Internal use only: nothing that
 * reaches the browser may include `rpcPass` (see describe()).
 *
 * Read once per process at start by config.js; the dashboard calls it again to
 * show what is saved.
 */
function resolve() {
  const saved = readFile();
  const env = fromEnvironment();
  if (!saved) {
    return { ...env, zmqHost: env.zmqHost || env.host, profile: profileForHost(env.host), source: 'environment', signature: '' };
  }
  const base = PROFILES[saved.profile] ? PROFILES[saved.profile].defaults : {};
  const host = pick(saved.host, base.host, env.host);
  return {
    host,
    rpcPort: Number(pick(saved.rpcPort, base.rpcPort, env.rpcPort)),
    rpcUser: pick(saved.rpcUser, base.rpcUser, ''),
    rpcPass: pick(saved.rpcPass, base.rpcPass, ''),
    zmqHost: pick(saved.zmqHost, base.zmqHost, host),
    zmqPort: Number(pick(saved.zmqPort, base.zmqPort, env.zmqPort)),
    profile: PROFILES[saved.profile] ? saved.profile : 'custom',
    source: 'file',
    signature: signature(),
  };
}

// ---------------------------------------------------------------------------
// Validation

const HOST_RE = /^[A-Za-z0-9]([A-Za-z0-9._-]{0,251}[A-Za-z0-9])?$/;
const IPV6_RE = /^[0-9A-Fa-f:.]{2,45}$/;
const CONTROL_RE = /[\u0000-\u001f\u007f]/;

function port(value, name) {
  const n = Number(value);
  if (!Number.isInteger(n) || n < 1 || n > 65535) throw new Error(`${name} must be a whole number from 1 to 65535`);
  return n;
}

function hostName(value, name) {
  const v = String(value == null ? '' : value).trim();
  if (!v) throw new Error(`${name} is required`);
  if (!HOST_RE.test(v) && !(v.includes(':') && IPV6_RE.test(v))) {
    throw new Error(`${name} must be a host name, container name or IP address`);
  }
  return v;
}

function text(value, name, { allowColon = true } = {}) {
  const v = String(value == null ? '' : value);
  if (v.length > MAX_FIELD) throw new Error(`${name} is too long`);
  if (CONTROL_RE.test(v)) throw new Error(`${name} contains characters that are not allowed`);
  if (!allowColon && v.includes(':')) throw new Error(`${name} cannot contain ":"`);
  return v;
}

/**
 * Turn what the browser sent into a complete, checked settings object.
 *
 * `current` is what is in force now; it supplies the password when the form
 * leaves it blank, but only if the host and port are unchanged. Re-using a
 * saved password against a different host would hand it to whatever the
 * operator typed there.
 *
 * Throws an Error with a message fit to show the operator.
 */
function normalise(input, current) {
  if (!input || typeof input !== 'object') throw new Error('settings must be an object');
  const profile = PROFILES[input.profile];
  if (!profile) throw new Error('unknown node software');
  const d = profile.defaults;

  const host = hostName(pick(input.host, d.host), 'Host');
  const rpcPort = port(pick(input.rpcPort, d.rpcPort), 'RPC port');
  const zmqPort = port(pick(input.zmqPort, d.zmqPort), 'ZMQ port');
  const zmqHostRaw = String(pick(input.zmqHost, d.zmqHost) || '').trim();
  const zmqHost = zmqHostRaw ? hostName(zmqHostRaw, 'ZMQ host') : host;

  let rpcUser;
  let rpcPass;
  if (profile.fixedLogin) {
    rpcUser = d.rpcUser;
    rpcPass = d.rpcPass;
  } else {
    rpcUser = text(pick(input.rpcUser, d.rpcUser, ''), 'RPC user', { allowColon: false });
    let pass = input.rpcPass == null ? '' : String(input.rpcPass);
    if (pass === '' && current && current.host === host && current.rpcPort === rpcPort) pass = current.rpcPass || '';
    rpcPass = text(pass, 'RPC password');
    if (!rpcUser) throw new Error('RPC user is required');
    if (!rpcPass) throw new Error('RPC password is required');
  }

  return { profile: profile.id, host, rpcPort, rpcUser, rpcPass, zmqHost, zmqPort };
}

/** Save atomically, readable by the app's user only. */
function save(settings) {
  const target = filePath();
  fs.mkdirSync(path.dirname(target), { recursive: true });
  const tmp = `${target}.${process.pid}.tmp`;
  const body = JSON.stringify({ ...settings, savedAt: new Date().toISOString() }, null, 2);
  fs.writeFileSync(tmp, body, { mode: 0o600 });
  fs.renameSync(tmp, target);
}

/** What the browser may see: everything except the password. */
function describe(settings) {
  const { rpcPass, signature: _sig, ...rest } = settings;
  return { ...rest, hasPassword: Boolean(rpcPass) };
}

function listProfiles() {
  return Object.values(PROFILES).map((p) => ({
    id: p.id,
    label: p.label,
    software: p.software,
    verified: p.verified,
    fixedLogin: p.fixedLogin,
    note: p.note,
    // Never the login of a profile whose login is fixed: the browser does not need it.
    defaults: { host: p.defaults.host, rpcPort: p.defaults.rpcPort, rpcUser: p.fixedLogin ? '' : p.defaults.rpcUser, zmqHost: p.defaults.zmqHost, zmqPort: p.defaults.zmqPort },
  }));
}

// ---------------------------------------------------------------------------
// Checking a node

let rpcFetch = rpcCall;

function rpcCall({ host, rpcPort, rpcUser, rpcPass }, method, timeoutMs) {
  return new Promise((resolve, reject) => {
    const body = JSON.stringify({ jsonrpc: '1.0', id: 'bchlab-check', method, params: [] });
    const auth = Buffer.from(`${rpcUser}:${rpcPass}`).toString('base64');
    const req = http.request(
      {
        hostname: host,
        port: rpcPort,
        path: '/',
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(body), Authorization: `Basic ${auth}` },
        timeout: timeoutMs,
      },
      (res) => {
        let raw = '';
        res.setEncoding('utf8');
        res.on('data', (c) => { raw += c; if (raw.length > 1_000_000) req.destroy(new Error('reply too large')); });
        res.on('end', () => {
          if (res.statusCode === 401 || res.statusCode === 403) return reject(Object.assign(new Error('the node refused the RPC user or password'), { code: 'EAUTH' }));
          let parsed;
          try { parsed = JSON.parse(raw); } catch { return reject(new Error(`unexpected reply (HTTP ${res.statusCode}) - is this an RPC port?`)); }
          if (parsed && parsed.error) return reject(new Error(`${method}: ${parsed.error.message || 'RPC error'}`));
          resolve(parsed && parsed.result);
        });
      },
    );
    req.on('timeout', () => req.destroy(Object.assign(new Error('timed out'), { code: 'ETIMEDOUT' })));
    req.on('error', reject);
    req.end(body);
  });
}

function friendly(err, host, rpcPort) {
  switch (err && err.code) {
    case 'ENOTFOUND': case 'EAI_AGAIN': return `no container or host called "${host}" was found`;
    case 'ECONNREFUSED': return `${host}:${rpcPort} refused the connection - wrong port, or the node is not running`;
    case 'ETIMEDOUT': return `${host}:${rpcPort} did not answer in time`;
    default: return err && err.message ? err.message : 'failed';
  }
}

function tcpOpen(host, portNumber, timeoutMs = 3000) {
  return new Promise((resolve) => {
    const socket = net.connect({ host, port: portNumber });
    const done = (ok) => { socket.destroy(); resolve(ok); };
    socket.setTimeout(timeoutMs, () => done(false));
    socket.once('connect', () => done(true));
    socket.once('error', () => done(false));
  });
}

/**
 * Is there a node here that this app can use?
 *
 * Read-only: getnetworkinfo, getblockchaininfo and a TCP connect to the ZMQ
 * port. ZMQ is only checked for "something is listening" - proving it publishes
 * blocks would mean waiting up to ten minutes for one.
 */
async function check(settings, { timeoutMs = 5000 } = {}) {
  const out = { ok: false, rpc: { ok: false }, zmq: { ok: false } };
  try {
    const net_ = await rpcFetch(settings, 'getnetworkinfo', timeoutMs);
    const chain = await rpcFetch(settings, 'getblockchaininfo', timeoutMs);
    out.rpc = {
      ok: true,
      subversion: net_ && net_.subversion ? String(net_.subversion) : null,
      connections: net_ && Number.isFinite(net_.connections) ? net_.connections : null,
      chain: chain && chain.chain ? String(chain.chain) : null,
      blocks: chain && Number.isFinite(chain.blocks) ? chain.blocks : null,
      syncing: chain ? Boolean(chain.initialblockdownload) : null,
    };
  } catch (err) {
    out.rpc = { ok: false, error: friendly(err, settings.host, settings.rpcPort) };
  }
  const zmqOk = await tcpOpen(settings.zmqHost, settings.zmqPort);
  out.zmq = zmqOk
    ? { ok: true }
    : { ok: false, error: `nothing is listening on ${settings.zmqHost}:${settings.zmqPort} - the node needs -zmqpubhashblock=tcp://0.0.0.0:${settings.zmqPort}` };
  out.ok = out.rpc.ok && out.zmq.ok;
  return out;
}

// ---------------------------------------------------------------------------
// Finding installed node apps

let lookup = (host) => dns.promises.lookup(host);

function resolves(host, timeoutMs = 2000) {
  return new Promise((resolve) => {
    const timer = setTimeout(() => resolve(false), timeoutMs);
    lookup(host).then(
      () => { clearTimeout(timer); resolve(true); },
      () => { clearTimeout(timer); resolve(false); },
    );
  });
}

/**
 * Which known node apps have a container answering on Umbrel's Docker network.
 * A name lookup and nothing else - no connection is opened.
 */
async function detect() {
  const found = [];
  for (const p of Object.values(PROFILES)) {
    for (const host of p.candidates) {
      // eslint-disable-next-line no-await-in-loop
      if (await resolves(host)) { found.push({ profile: p.id, host, verified: p.verified }); break; }
    }
  }
  return found;
}

// ---------------------------------------------------------------------------
// Applying a change

/**
 * Exit when the saved file changes so Docker restarts this process on the new
 * settings. `loaded` is the signature config.js saw at start. Unref'd: it must
 * never be the thing keeping a process alive.
 */
function watch(logger, loadedSignature) {
  if (process.env.NODE_PROFILE_WATCH === '0') return null;
  const timer = setInterval(() => {
    if (signature() !== loadedSignature) {
      logger.info('node settings changed - restarting to apply them');
      process.exit(1);
    }
  }, WATCH_INTERVAL_MS);
  if (timer.unref) timer.unref();
  return timer;
}

module.exports = {
  PROFILES,
  FILE_NAME,
  filePath,
  resolve,
  normalise,
  save,
  describe,
  listProfiles,
  check,
  detect,
  watch,
  signature,
  _setRpc: (fn) => { rpcFetch = fn || rpcCall; },
  _setLookup: (fn) => { lookup = fn || ((h) => dns.promises.lookup(h)); },
};
