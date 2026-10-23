'use strict';

/**
 * Bitcoin Cash writes one address two ways: legacy base58 (`1Bp...`) and
 * CashAddr (`bitcoincash:qp...`). The same 20-byte hash sits inside both.
 * Pools disagree on which they accept as a stratum login - 2Miners' BCH pools
 * reject the legacy form with "Invalid address", Kryptex accepts it - so the
 * stratum client needs to be able to offer either form of the same address.
 *
 * Only pay-to-pubkey-hash and pay-to-script-hash addresses are handled, which
 * is all a login needs.
 */

const crypto = require('crypto');

const B58 = '123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz';
const CHARSET = 'qpzry9x8gf2tvdw0s3jn54khce6mua7l';
const PREFIX = 'bitcoincash';

function sha256d(buf) {
  return crypto.createHash('sha256').update(crypto.createHash('sha256').update(buf).digest()).digest();
}

/** Legacy address -> { type: 0|1, hash: Buffer(20) }, or null if it is not a valid one. */
function decodeLegacy(address) {
  if (typeof address !== 'string' || !address) return null;
  let n = 0n;
  for (const ch of address) {
    const v = B58.indexOf(ch);
    if (v < 0) return null;
    n = n * 58n + BigInt(v);
  }
  let hex = n.toString(16);
  if (hex.length % 2) hex = `0${hex}`;
  let bytes = Buffer.from(hex, 'hex');
  const zeros = address.match(/^1*/)[0].length;
  bytes = Buffer.concat([Buffer.alloc(zeros), bytes]);
  if (bytes.length !== 25) return null;
  const body = bytes.subarray(0, 21);
  if (!sha256d(body).subarray(0, 4).equals(bytes.subarray(21))) return null;
  const version = body[0];
  if (version !== 0 && version !== 5) return null;
  return { type: version === 0 ? 0 : 1, hash: Buffer.from(body.subarray(1)) };
}

function polymod(values) {
  const GEN = [0x98f2bc8e61n, 0x79b76d99e2n, 0xf33e5fb3c4n, 0xae2eabe2a8n, 0x1e4f43e470n];
  let c = 1n;
  for (const v of values) {
    const c0 = c >> 35n;
    c = ((c & 0x07ffffffffn) << 5n) ^ BigInt(v);
    for (let i = 0; i < 5; i += 1) if ((c0 >> BigInt(i)) & 1n) c ^= GEN[i];
  }
  return c ^ 1n;
}

function toFiveBit(bytes) {
  const out = [];
  let acc = 0;
  let bits = 0;
  for (const b of bytes) {
    acc = (acc << 8) | b;
    bits += 8;
    while (bits >= 5) { bits -= 5; out.push((acc >> bits) & 31); }
    acc &= (1 << bits) - 1;
  }
  if (bits > 0) out.push((acc << (5 - bits)) & 31);
  return out;
}

/** { type, hash } -> `bitcoincash:q...` */
function encodeCashAddr({ type, hash }) {
  const versionByte = (type << 3) | 0; // 160-bit hash
  const payload = toFiveBit(Buffer.concat([Buffer.from([versionByte]), hash]));
  const prefixValues = [...PREFIX].map((c) => c.charCodeAt(0) & 31);
  const checksum = polymod([...prefixValues, 0, ...payload, 0, 0, 0, 0, 0, 0, 0, 0]);
  const check = [];
  for (let i = 0; i < 8; i += 1) check.push(Number((checksum >> BigInt(5 * (7 - i))) & 31n));
  return `${PREFIX}:${[...payload, ...check].map((v) => CHARSET[v]).join('')}`;
}

/** Legacy -> CashAddr; null if the input is not a valid legacy address. */
function legacyToCashAddr(address) {
  const d = decodeLegacy(address);
  return d ? encodeCashAddr(d) : null;
}

/**
 * Every form of this login address worth offering, preferred first. A
 * CashAddr (or anything this cannot parse) is returned as given.
 */
function loginForms(address) {
  const cash = legacyToCashAddr(address);
  return cash ? [address, cash] : [address];
}

module.exports = { decodeLegacy, legacyToCashAddr, loginForms };
