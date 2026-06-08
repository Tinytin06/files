<script lang="ts">
	import { onMount } from 'svelte';
	import Cryptex from '$lib/Cryptex.svelte';
	import {
		unlock,
		downloadPhoto,
		adminUploadPhoto,
		changeCombination,
		getConfig,
		lock
	} from '$lib/api';

	let status = $state<'locked' | 'unlocked'>('locked');
	let message = $state('Rotate the rings and unlock.');
	let busy = $state(false);

	// UI shape comes from the server (set via CRYPTEX_RINGS / CRYPTEX_ALPHABET).
	let rings = $state(5);
	let alphabet = $state('ABCDEFGHIJKLMNOPQRSTUVWXYZ');

	onMount(async () => {
		const c = await getConfig();
		rings = c.rings;
		alphabet = c.alphabet;
	});

	// admin / change-combination panel
	let showAdmin = $state(false);
	let adminToken = $state('');
	let newCombo = $state('');

	async function onSubmit(guess: string) {
		busy = true;
		message = 'Checking…';
		const { ok, rateLimited } = await unlock(guess);
		busy = false;
		if (ok) {
			status = 'unlocked';
			message = 'Unlocked.';
		} else if (rateLimited) {
			message = 'Too many attempts — slow down and try again.';
		} else {
			message = 'Wrong combination.';
		}
	}

	async function onDownload() {
		busy = true;
		const ok = await downloadPhoto();
		busy = false;
		message = ok ? 'Photo downloaded.' : 'Download failed (token may have expired).';
	}

	async function onAdminUpload(e: Event) {
		const input = e.target as HTMLInputElement;
		const file = input.files?.[0];
		if (!file) return;
		busy = true;
		const { ok, status: code } = await adminUploadPhoto(file, adminToken);
		busy = false;
		message = ok
			? 'Photo uploaded.'
			: code === 403
				? 'Admin token rejected.'
				: code === 415
					? 'Not a supported image.'
					: 'Upload failed.';
		input.value = '';
	}

	async function onChangeCombo() {
		busy = true;
		const { ok, status: code } = await changeCombination(newCombo, adminToken);
		busy = false;
		message = ok
			? 'Combination changed.'
			: code === 403
				? 'Admin token rejected.'
				: 'Change failed.';
		if (ok) {
			newCombo = '';
			// Re-pull config so the rings reshape to the new combination length.
			const c = await getConfig();
			rings = c.rings;
			alphabet = c.alphabet;
		}
	}

	function onLock() {
		lock();
		status = 'locked';
		message = 'Locked.';
	}
</script>

<main>
	<h1>Cryptex</h1>

	{#if status === 'locked'}
		<Cryptex {rings} {alphabet} disabled={busy} onsubmit={onSubmit} />
	{:else}
		<section class="vault">
			<p class="unlocked-banner">🔓 Unlocked</p>
			<div class="actions">
				<button onclick={onDownload} disabled={busy}>Download photo</button>
				<button class="secondary" onclick={onLock}>Lock</button>
			</div>
		</section>
	{/if}

	<p class="message" aria-live="polite">{message}</p>

	<details class="admin" bind:open={showAdmin}>
		<summary>Change combination (admin)</summary>
		<div class="admin-body">
			<p class="hint">
				Requires the admin token configured on the server. The combination is hashed
				server-side; the plaintext is never stored or returned.
			</p>
			<input type="password" placeholder="Admin token" bind:value={adminToken} />

			<label class="filebtn admin-upload" class:disabled={busy || !adminToken}>
				Upload / replace photo
				<input
					type="file"
					accept="image/*"
					onchange={onAdminUpload}
					disabled={busy || !adminToken}
				/>
			</label>

			<hr />

			<input
				type="text"
				class="combo-input"
				placeholder="New combination (e.g. APPLE)"
				autocapitalize="characters"
				value={newCombo}
				oninput={(e) => (newCombo = e.currentTarget.value.toUpperCase())}
			/>
			<button onclick={onChangeCombo} disabled={busy || !adminToken || !newCombo}>
				Set new combination
			</button>
		</div>
	</details>
</main>

<style>
	:global(body) {
		margin: 0;
		background: radial-gradient(circle at 50% 0%, #2a1d12, #120c07);
		color: #e8d9c0;
		font-family: system-ui, sans-serif;
		min-height: 100vh;
	}
	main {
		max-width: 520px;
		margin: 0 auto;
		padding: 2.5rem 1.25rem 4rem;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 1.75rem;
	}
	h1 {
		font-weight: 800;
		letter-spacing: 0.15em;
		text-transform: uppercase;
		color: #f3d79a;
		margin: 0;
	}
	.vault {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 1.25rem;
	}
	.unlocked-banner {
		font-size: 1.4rem;
		margin: 0;
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: 0.75rem;
		justify-content: center;
	}
	button,
	.filebtn {
		padding: 0.6rem 1.4rem;
		font-size: 0.95rem;
		font-weight: 600;
		color: #2a1d12;
		background: linear-gradient(180deg, #f3d79a, #d9a441);
		border: none;
		border-radius: 8px;
		cursor: pointer;
	}
	button.secondary {
		background: #3a2a1a;
		color: #e8d9c0;
	}
	button:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
	.filebtn input {
		display: none;
	}
	.message {
		min-height: 1.2em;
		color: #c9a36a;
		font-size: 0.95rem;
	}
	.admin {
		width: 100%;
		max-width: 360px;
		border: 1px solid #4a3826;
		border-radius: 8px;
		padding: 0.5rem 0.85rem;
	}
	.admin summary {
		cursor: pointer;
		color: #c9a36a;
		font-size: 0.9rem;
	}
	.admin-body {
		display: flex;
		flex-direction: column;
		gap: 0.6rem;
		padding-top: 0.75rem;
	}
	.admin-body input {
		padding: 0.55rem 0.75rem;
		border-radius: 6px;
		border: 1px solid #4a3826;
		background: #1c140d;
		color: #e8d9c0;
		font-size: 0.9rem;
	}
	.hint {
		font-size: 0.8rem;
		color: #8a7355;
		margin: 0;
		line-height: 1.4;
	}
	.combo-input {
		text-transform: uppercase;
		letter-spacing: 0.1em;
	}
	.admin-upload {
		text-align: center;
	}
	.admin-upload.disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
	.admin-body hr {
		width: 100%;
		border: none;
		border-top: 1px solid #4a3826;
		margin: 0.25rem 0;
	}
</style>
