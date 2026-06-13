<script lang="ts">
	import { onMount } from 'svelte';
	import Cryptex from '$lib/Cryptex.svelte';
	import {
		unlock,
		downloadPhoto,
		getConfig,
		lock,
		listEntries,
		createEntry,
		uploadEntryFile,
		deleteEntry,
		type Entry
	} from '$lib/api';

	let status = $state<'locked' | 'unlocked'>('locked');
	let message = $state('Rotate the rings and unlock.');
	let busy = $state(false);

	// UI shape comes from the server (uniform combination length / alphabet).
	let rings = $state(5);
	let alphabet = $state('ABCDEFGHIJKLMNOPQRSTUVWXYZ');

	onMount(refreshConfig);

	async function refreshConfig() {
		const c = await getConfig();
		rings = c.rings;
		alphabet = c.alphabet;
	}

	// admin panel
	let showAdmin = $state(false);
	let adminToken = $state('');
	let entries = $state<Entry[]>([]);
	let loaded = $state(false);
	// "add new combination" form
	let newLabel = $state('');
	let newCombo = $state('');
	let newFile = $state<File | null>(null);

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
		message = ok ? 'File downloaded.' : 'Download failed (token may have expired).';
	}

	function onLock() {
		lock();
		status = 'locked';
		message = 'Locked.';
	}

	// --- admin ---

	async function loadEntries() {
		if (!adminToken) return;
		busy = true;
		entries = await listEntries(adminToken);
		loaded = true;
		busy = false;
	}

	async function onAddEntry() {
		busy = true;
		const { ok, status: code, id } = await createEntry(newLabel, newCombo, adminToken);
		if (ok && id && newFile) {
			const up = await uploadEntryFile(id, newFile, adminToken);
			message = up.ok
				? 'Combination added with file.'
				: 'Combination added, but file upload failed.';
		} else if (ok) {
			message = 'Combination added (no file yet).';
		} else {
			message =
				code === 409
					? 'That combination already exists.'
					: code === 422
						? `Combination must be ${rings} characters.`
						: code === 403
							? 'Admin token rejected.'
							: 'Could not add combination.';
		}
		busy = false;
		if (ok) {
			newLabel = '';
			newCombo = '';
			newFile = null;
			await loadEntries();
			await refreshConfig();
		}
	}

	async function onReplaceFile(id: string, e: Event) {
		const input = e.target as HTMLInputElement;
		const file = input.files?.[0];
		if (!file) return;
		busy = true;
		const { ok } = await uploadEntryFile(id, file, adminToken);
		busy = false;
		message = ok ? 'File replaced.' : 'Replace failed.';
		input.value = '';
		await loadEntries();
	}

	async function onDelete(id: string, label: string) {
		if (!confirm(`Delete combination "${label || id}" and its file?`)) return;
		busy = true;
		const { ok } = await deleteEntry(id, adminToken);
		busy = false;
		message = ok ? 'Combination deleted.' : 'Delete failed.';
		await loadEntries();
		await refreshConfig();
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
		<summary>Manage combinations (admin)</summary>
		<div class="admin-body">
			<p class="hint">
				Requires the admin token configured on the server. Each combination opens its
				own file. Combinations are hashed server-side and sealed with ML-KEM-768 in
				transit; the plaintext is never stored or returned.
			</p>

			<div class="row">
				<input
					type="password"
					placeholder="Admin token"
					bind:value={adminToken}
					onkeydown={(e) => e.key === 'Enter' && loadEntries()}
				/>
				<button onclick={loadEntries} disabled={busy || !adminToken}>Load</button>
			</div>

			{#if loaded}
				<ul class="entries">
					{#each entries as entry (entry.id)}
						<li>
							<span class="entry-label">
								{entry.label || '(unnamed)'}
								{#if !entry.has_file}<em class="nofile">no file</em>{/if}
							</span>
							<label class="filebtn small" class:disabled={busy}>
								Replace file
								<input
									type="file"
									onchange={(e) => onReplaceFile(entry.id, e)}
									disabled={busy}
								/>
							</label>
							<button class="danger" onclick={() => onDelete(entry.id, entry.label)} disabled={busy}>
								Delete
							</button>
						</li>
					{:else}
						<li class="empty">No combinations yet — add one below.</li>
					{/each}
				</ul>

				<hr />

				<p class="hint">Add a new combination ({rings} characters):</p>
				<input type="text" placeholder="Label (e.g. Birthday photo)" bind:value={newLabel} />
				<input
					type="text"
					class="combo-input"
					placeholder="Combination (e.g. APPLE)"
					autocapitalize="characters"
					value={newCombo}
					oninput={(e) => (newCombo = e.currentTarget.value.toUpperCase())}
				/>
				<label class="filebtn admin-upload" class:disabled={busy}>
					{newFile ? newFile.name : 'Choose file (optional)'}
					<input
						type="file"
						onchange={(e) => (newFile = (e.currentTarget as HTMLInputElement).files?.[0] ?? null)}
						disabled={busy}
					/>
				</label>
				<button onclick={onAddEntry} disabled={busy || !newCombo}>Add combination</button>
			{/if}
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
	.row {
		display: flex;
		gap: 0.5rem;
	}
	.row input {
		flex: 1;
	}
	.entries {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
	}
	.entries li {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		background: #1c140d;
		border: 1px solid #4a3826;
		border-radius: 6px;
		padding: 0.4rem 0.6rem;
	}
	.entries li.empty {
		justify-content: center;
		color: #8a7355;
		font-size: 0.85rem;
	}
	.entry-label {
		flex: 1;
		font-size: 0.9rem;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.nofile {
		color: #8a7355;
		font-size: 0.75rem;
		margin-left: 0.35rem;
	}
	.filebtn.small,
	button.danger {
		padding: 0.35rem 0.7rem;
		font-size: 0.8rem;
	}
	button.danger {
		background: #5a2a2a;
		color: #f0d4d4;
	}
	.filebtn.disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
</style>
