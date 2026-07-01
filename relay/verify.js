'use strict';
// Ed25519 verification for Discord interaction webhooks. Dependency-free: uses
// node:crypto JWK import so no tweetnacl. Discord signs UTF8(timestamp + rawBody)
// with the application's private key; we verify with the app public key (hex).
const crypto = require('crypto');

function pubKeyFromHex(hex) {
  const raw = Buffer.from(hex, 'hex');
  if (raw.length !== 32) throw new Error('ed25519 public key must be 32 bytes');
  return crypto.createPublicKey({
    key: { kty: 'OKP', crv: 'Ed25519', x: raw.toString('base64url') },
    format: 'jwk',
  });
}

// verify returns true only for a well-formed, correct signature.
function verify(publicKeyHex, timestamp, rawBody, signatureHex) {
  try {
    const key = pubKeyFromHex(publicKeyHex);
    const sig = Buffer.from(signatureHex, 'hex');
    if (sig.length !== 64) return false;
    return crypto.verify(null, Buffer.from(String(timestamp) + rawBody, 'utf8'), key, sig);
  } catch (_e) {
    return false;
  }
}

module.exports = { verify, pubKeyFromHex };
