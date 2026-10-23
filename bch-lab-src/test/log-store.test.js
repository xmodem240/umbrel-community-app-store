'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { readRecent } = require('../src/lib/log-store');

const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'bchlab-logs-'));
fs.writeFileSync(path.join(dir, 'relay-profiler.log'), [
  '2026-10-23T10:20:02.519Z [INFO] [relay-profiler] block race recorded {"eligible":8,"first":2}',
  '2026-10-23T10:21:00.000Z [WARN] [relay-profiler] getblockheader failed - counting the block as usual {"error":"x"}',
  '',
].join('\n'));
fs.writeFileSync(path.join(dir, 'relay-profiler.log.1'),
  '2026-10-23T09:00:00.000Z [INFO] [relay-profiler] older line from the rotated file\n');
fs.writeFileSync(path.join(dir, 'dashboard.log'),
  '2026-10-23T10:20:30.000Z [ERROR] [dashboard] something broke\nnot a structured line\n');
fs.writeFileSync(path.join(dir, 'notes.txt'), 'must be ignored');

test('merges services oldest first, including the rotated file', () => {
  const { lines, services } = readRecent({ dir });
  assert.deepEqual(services, ['dashboard', 'relay-profiler']);
  assert.equal(lines[0].message, 'older line from the rotated file');
  assert.equal(lines.some((l) => l.message === 'not a structured line' && l.level === 'ERROR'), true);
  assert.equal(lines.at(-1).level, 'WARN');
});

test('level is a minimum', () => {
  assert.deepEqual(readRecent({ dir, level: 'warn' }).lines.map((l) => l.level).sort(), ['ERROR', 'ERROR', 'WARN']);
});

test('service and search filters', () => {
  assert.equal(readRecent({ dir, service: 'dashboard' }).lines.every((l) => l.service === 'dashboard'), true);
  const hit = readRecent({ dir, q: 'RACE RECORDED' }).lines;
  assert.equal(hit.length, 1);
  assert.match(hit[0].message, /eligible/);
});

test('limit keeps the newest lines', () => {
  const { lines } = readRecent({ dir, limit: 2 });
  assert.equal(lines.length, 2);
  assert.equal(lines.at(-1).message.startsWith('getblockheader'), true);
});

test('unstructured line is kept, not dropped', () => {
  assert.ok(readRecent({ dir, service: 'dashboard' }).lines.some((l) => l.message === 'not a structured line'));
});

test('missing directory is an empty result, not an error', () => {
  assert.deepEqual(readRecent({ dir: path.join(dir, 'nope') }), { lines: [], services: [] });
});

test('logger writes a line the reader can parse', () => {
  const d = fs.mkdtempSync(path.join(os.tmpdir(), 'bchlab-logs2-'));
  const out = require('child_process').spawnSync(process.execPath, ['-e',
    "require('./src/lib/logger').make('t').info('hello world', {a:1})"],
  { cwd: path.join(__dirname, '..'), env: { ...process.env, LOG_DIR: d, LOG_LEVEL: 'info' } });
  assert.equal(out.status, 0);
  const { lines } = readRecent({ dir: d });
  assert.equal(lines.length, 1);
  assert.equal(lines[0].message, 'hello world {"a":1}');
  assert.equal(lines[0].scope, 't');
});
