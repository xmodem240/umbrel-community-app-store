'use strict';

/**
 * A copy of each process's log lines on the shared data volume, so the
 * dashboard can show them.
 *
 * The app is four containers (dashboard, peer-profiler, relay-profiler,
 * stratum-race) and a container can only see its own stdout. They do share
 * /data, so every process appends to its own file there - one writer per file,
 * no locking - and the dashboard reads them all.
 *
 * Bounded on purpose: a file is rotated at MAX_BYTES and one old copy is kept,
 * so four services use at most about 8 MB. Writing is best-effort; a log line
 * that cannot be saved must never take a service down.
 */

const fs = require('fs');
const path = require('path');

const MAX_BYTES = 1024 * 1024;
const CHECK_EVERY = 100; // writes between size checks
const LEVEL_RANK = { DEBUG: 0, INFO: 1, WARN: 2, ERROR: 3 };
const LINE = /^(\d{4}-\d\d-\d\dT[\d:.]+Z) \[(\w+)\] \[([^\]]*)\] (.*)$/;

function logDir() {
  return process.env.LOG_DIR || path.join(process.env.DATA_DIR || '/data', 'logs');
}

// "relay-profiler.js" -> "relay-profiler"; "dashboard-server.js" -> "dashboard".
function serviceName() {
  const base = path.basename(process.argv[1] || 'app', '.js');
  return base === 'dashboard-server' ? 'dashboard' : base;
}

let writes = 0;
let ready = false;
let file = null;

function append(line) {
  try {
    if (!ready) {
      const dir = logDir();
      fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
      file = path.join(dir, `${serviceName()}.log`);
      ready = true;
    }
    if (++writes % CHECK_EVERY === 0) {
      try {
        if (fs.statSync(file).size > MAX_BYTES) fs.renameSync(file, `${file}.1`);
      } catch { /* file not there yet */ }
    }
    fs.appendFileSync(file, `${line}\n`, { mode: 0o600 });
  } catch {
    ready = ready && Boolean(file); // keep trying on the next line, never throw
  }
}

function parseLine(raw, service) {
  const m = LINE.exec(raw);
  if (!m) return { ts: null, level: 'INFO', scope: '', service, message: raw };
  return { ts: m[1], level: m[2], scope: m[3], service, message: m[4] };
}

/**
 * The newest lines across all services (or one), oldest first.
 * level is a minimum; q is a case-insensitive substring.
 */
function readRecent({ service = 'all', level = 'info', limit = 300, q = '', dir = logDir() } = {}) {
  const min = LEVEL_RANK[String(level).toUpperCase()] ?? 1;
  const needle = String(q || '').toLowerCase();
  const cap = Math.max(1, Math.min(2000, Number(limit) || 300));
  let names;
  try {
    names = fs.readdirSync(dir).filter((n) => /^[a-z0-9-]+\.log(\.1)?$/.test(n));
  } catch {
    return { lines: [], services: [] };
  }
  const services = [...new Set(names.map((n) => n.replace(/\.log(\.1)?$/, '')))].sort();
  const lines = [];
  for (const name of names) {
    const svc = name.replace(/\.log(\.1)?$/, '');
    if (service !== 'all' && service !== svc) continue;
    let text;
    try { text = fs.readFileSync(path.join(dir, name), 'utf8'); } catch { continue; }
    let prev = null;
    for (const raw of text.split('\n')) {
      if (!raw) continue;
      const entry = parseLine(raw, svc);
      // A line with no timestamp (the rest of a stack trace) belongs to the
      // line before it: same time, same level, so it sorts and filters with it.
      if (entry.ts === null && prev) { entry.ts = prev.ts; entry.level = prev.level; }
      prev = entry;
      if ((LEVEL_RANK[entry.level] ?? 1) < min) continue;
      if (needle && !raw.toLowerCase().includes(needle)) continue;
      lines.push(entry);
    }
  }
  lines.sort((a, b) => (a.ts || '').localeCompare(b.ts || ''));
  return { lines: lines.slice(-cap), services };
}

module.exports = { append, readRecent, serviceName, logDir };
