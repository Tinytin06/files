// REST client for the cryptex API. The client only ever observes HTTP status
// codes — it never receives, stores, or infers the real password. Secrets sent
// to the server (the guess, a new combination) are sealed with ML-KEM-768.
//
// Base URL resolution:
//   - Website build: defaults to "/api" (same origin as the served SPA).
//   - Mobile build:  set VITE_API_BASE_URL=https://your-host/api at build time,
//     because the native shell has no same-origin server to talk to.
import { seal } from './crypto';

const BASE: string = import.meta.env.VITE_API_BASE_URL ?? '/api';

let token: string | null = null;

export interface CryptexConfig {
	rings: number;
	alphabet: string;
}

export interface Entry {
	id: string;
	label: string;
	len: number;
	has_file: boolean;
}

const DEFAULT_CONFIG: CryptexConfig = { rings: 5, alphabet: 'ABCDEFGHIJKLMNOPQRSTUVWXYZ' };

/** Fetch the UI shape (ring count + alphabet) from the server. */
export async function getConfig(): Promise<CryptexConfig> {
	try {
		const res = await fetch(`${BASE}/config`);
		if (res.ok) {
			const c = await res.json();
			if (typeof c.rings === 'number' && typeof c.alphabet === 'string' && c.alphabet) {
				return c;
			}
		}
	} catch {
		// fall through to default
	}
	return DEFAULT_CONFIG;
}

export function lock(): void {
	token = null;
}

/**
 * Submit a guess (sealed with ML-KEM-768). On 200 the token internally
 * references whichever entry matched — the client never learns which one.
 * 200 = unlocked, 401 = wrong.
 */
export async function unlock(guess: string): Promise<{ ok: boolean; rateLimited: boolean }> {
	try {
		const env = await seal(BASE, guess);
		const res = await fetch(`${BASE}/unlock`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify(env)
		});
		if (res.status === 200) {
			const data = await res.json();
			token = data.token;
			return { ok: true, rateLimited: false };
		}
		return { ok: false, rateLimited: res.status === 429 };
	} catch {
		// Network/crypto failure (e.g. server unreachable, or crypto.subtle
		// unavailable on a non-secure origin). Treat as a failed attempt rather
		// than letting the rejection escape and freeze the UI.
		return { ok: false, rateLimited: false };
	}
}

/** Download the protected photo and trigger a browser save. */
export async function downloadPhoto(): Promise<boolean> {
	if (!token) return false;
	const res = await fetch(`${BASE}/photo`, {
		headers: { Authorization: `Bearer ${token}` }
	});
	if (!res.ok) return false;
	const blob = await res.blob();
	const filename = filenameFromDisposition(res.headers.get('Content-Disposition')) ?? 'secret';
	const url = URL.createObjectURL(blob);
	const a = document.createElement('a');
	a.href = url;
	a.download = filename;
	a.click();
	URL.revokeObjectURL(url);
	return true;
}

// --- admin entry management (all require the admin token) ---

/** List entries (id, label, length, whether a file is uploaded). */
export async function listEntries(adminToken: string): Promise<Entry[]> {
	const res = await fetch(`${BASE}/entries`, {
		headers: { Authorization: `Bearer ${adminToken}` }
	});
	if (!res.ok) return [];
	return res.json();
}

/**
 * Create a new combination (sealed with ML-KEM-768) with a label. Returns the
 * new entry id, or a status: 409 = duplicate combo, 422 = wrong length.
 */
export async function createEntry(
	label: string,
	combination: string,
	adminToken: string
): Promise<{ ok: boolean; status: number; id?: string }> {
	try {
		const combo = await seal(BASE, combination);
		const res = await fetch(`${BASE}/entries`, {
			method: 'POST',
			headers: {
				'Content-Type': 'application/json',
				Authorization: `Bearer ${adminToken}`
			},
			body: JSON.stringify({ label, combo })
		});
		let id: string | undefined;
		if (res.ok) id = (await res.json()).id;
		return { ok: res.ok, status: res.status, id };
	} catch {
		// Network/crypto failure — report a non-ok result instead of throwing,
		// so the caller can show a message rather than freezing.
		return { ok: false, status: 0 };
	}
}

/** Upload/replace the file for an entry. Any file type is accepted. */
export async function uploadEntryFile(
	id: string,
	file: File,
	adminToken: string
): Promise<{ ok: boolean; status: number }> {
	try {
		const res = await fetch(`${BASE}/entries/${id}/file`, {
			method: 'PUT',
			headers: {
				Authorization: `Bearer ${adminToken}`,
				// Percent-encoded so non-ASCII names survive the header; the server
				// sanitizes it. Lets the download preserve the original filename.
				'X-Filename': encodeURIComponent(file.name)
			},
			body: file
		});
		return { ok: res.ok, status: res.status };
	} catch {
		return { ok: false, status: 0 };
	}
}

/** Delete an entry (its combination and file). */
export async function deleteEntry(
	id: string,
	adminToken: string
): Promise<{ ok: boolean; status: number }> {
	const res = await fetch(`${BASE}/entries/${id}`, {
		method: 'DELETE',
		headers: { Authorization: `Bearer ${adminToken}` }
	});
	return { ok: res.ok, status: res.status };
}

function filenameFromDisposition(header: string | null): string | null {
	if (!header) return null;
	const m = /filename="?([^"]+)"?/.exec(header);
	return m ? m[1] : null;
}
