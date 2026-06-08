// REST client for the cryptex API. The client only ever observes HTTP status
// codes — it never receives, stores, or infers the real password.
//
// Base URL resolution:
//   - Website build: defaults to "/api" (same origin as the served SPA).
//   - Mobile build:  set VITE_API_BASE_URL=https://your-host/api at build time,
//     because the native shell has no same-origin server to talk to.
const BASE: string = import.meta.env.VITE_API_BASE_URL ?? '/api';

let token: string | null = null;
let scope: string | null = null;

export interface CryptexConfig {
	rings: number;
	alphabet: string;
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

export function isUnlocked(): boolean {
	return token !== null;
}

export function canWrite(): boolean {
	return scope === 'read+write';
}

export function lock(): void {
	token = null;
	scope = null;
}

/** Submit a guess. Returns true on 200 (unlocked), false on 401. */
export async function unlock(guess: string): Promise<{ ok: boolean; rateLimited: boolean }> {
	const res = await fetch(`${BASE}/unlock`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ guess })
	});
	if (res.status === 200) {
		const data = await res.json();
		token = data.token;
		scope = data.scope;
		return { ok: true, rateLimited: false };
	}
	return { ok: false, rateLimited: res.status === 429 };
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

/** Replace the protected photo (requires a write-scoped token). */
export async function replacePhoto(file: File): Promise<{ ok: boolean; status: number }> {
	if (!token) return { ok: false, status: 401 };
	const res = await fetch(`${BASE}/photo`, {
		method: 'PUT',
		headers: { Authorization: `Bearer ${token}` },
		body: file
	});
	return { ok: res.ok, status: res.status };
}

/** Upload/replace the photo using the admin token (no unlock required). */
export async function adminUploadPhoto(
	file: File,
	adminToken: string
): Promise<{ ok: boolean; status: number }> {
	const res = await fetch(`${BASE}/photo`, {
		method: 'PUT',
		headers: { Authorization: `Bearer ${adminToken}` },
		body: file
	});
	return { ok: res.ok, status: res.status };
}

/** Change the combination. Requires the admin token (stronger than a read). */
export async function changeCombination(
	newCombination: string,
	adminToken: string
): Promise<{ ok: boolean; status: number }> {
	const res = await fetch(`${BASE}/password`, {
		method: 'POST',
		headers: {
			'Content-Type': 'application/json',
			Authorization: `Bearer ${adminToken}`
		},
		body: JSON.stringify({ new_combination: newCombination })
	});
	return { ok: res.ok, status: res.status };
}

function filenameFromDisposition(header: string | null): string | null {
	if (!header) return null;
	const m = /filename="?([^"]+)"?/.exec(header);
	return m ? m[1] : null;
}
