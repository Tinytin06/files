// Post-quantum sealing of the secret before it leaves the browser.
//
// The server publishes an ML-KEM-768 (FIPS 203) public key. We encapsulate a
// fresh 32-byte shared secret to it, use that as an AES-256-GCM key to encrypt
// the value, and send { kem, nonce, ciphertext }. Only the server's private key
// can decapsulate the shared secret, so the password is protected even if TLS
// were stripped. The plaintext never appears in the request body.
import { ml_kem768 } from '@noble/post-quantum/ml-kem.js';

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

	const key = await crypto.subtle.importKey('raw', sharedSecret, 'AES-GCM', false, ['encrypt']);
	const nonce = crypto.getRandomValues(new Uint8Array(12));
	const ct = new Uint8Array(
		await crypto.subtle.encrypt(
			{ name: 'AES-GCM', iv: nonce },
			key,
			new TextEncoder().encode(plaintext)
		)
	);

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
