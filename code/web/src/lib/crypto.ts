// Post-quantum sealing of the secret before it leaves the browser.
//
// The server publishes an ML-KEM-768 (FIPS 203) public key. We encapsulate a
// fresh 32-byte shared secret to it, use that as an AES-256-GCM key to encrypt
// the value, and send { kem, nonce, ciphertext }. Only the server's private key
// can decapsulate the shared secret, so the password is protected even if TLS
// were stripped. The plaintext never appears in the request body.
//
// AES-GCM uses a pure-JS implementation (@noble/ciphers), NOT the Web Crypto
// `crypto.subtle` API. `crypto.subtle` is only available in a secure context
// (HTTPS or localhost); on a plain-HTTP LAN deployment (e.g. http://<nas-ip>)
// it is undefined and any seal would throw before sending. Since this app is
// meant to run on a LAN and the ML-KEM envelope is what actually protects the
// secret, the sealing must not depend on the transport being a secure context.
import { ml_kem768 } from '@noble/post-quantum/ml-kem.js';
import { gcm } from '@noble/ciphers/aes.js';

export interface Envelope {
	kem: string;
	nonce: string;
	ciphertext: string;
}

let cachedKey: Uint8Array | null = null;

/** Fetch (and cache) the server's ML-KEM-768 public key. */
async function publicKey(base: string): Promise<Uint8Array> {
	if (cachedKey) return cachedKey;
	const res = await fetch(`${base}/kem`);
	if (!res.ok) throw new Error('could not fetch KEM public key');
	const { public_key } = await res.json();
	cachedKey = b64ToBytes(public_key);
	return cachedKey;
}

/** Seal a value to the server's public key. */
export async function seal(base: string, plaintext: string): Promise<Envelope> {
	const pk = await publicKey(base);
	const { cipherText, sharedSecret } = ml_kem768.encapsulate(pk);

	// AES-256-GCM under the 32-byte shared secret. noble's gcm().encrypt returns
	// ciphertext||tag (16-byte tag), matching Go's crypto/cipher GCM Seal output.
	const nonce = crypto.getRandomValues(new Uint8Array(12));
	const ct = gcm(sharedSecret, nonce).encrypt(new TextEncoder().encode(plaintext));

	return {
		kem: bytesToB64(cipherText),
		nonce: bytesToB64(nonce),
		ciphertext: bytesToB64(ct)
	};
}

function bytesToB64(b: Uint8Array): string {
	let s = '';
	for (const x of b) s += String.fromCharCode(x);
	return btoa(s);
}

function b64ToBytes(s: string): Uint8Array {
	const bin = atob(s);
	const out = new Uint8Array(bin.length);
	for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
	return out;
}
