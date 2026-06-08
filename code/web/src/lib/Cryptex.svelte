<script lang="ts">
	// A rotatable cryptex. Each ring is a wheel of characters; the user rotates
	// rings up/down to dial in a combination. Ring positions live in local
	// state only — the guess is assembled here and never compared on the client.
	interface Props {
		rings?: number;
		alphabet?: string;
		disabled?: boolean;
		onsubmit?: (guess: string) => void;
	}

	let {
		rings = 5,
		alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ',
		disabled = false,
		onsubmit
	}: Props = $props();

	const chars = alphabet.split('');
	// Index into `chars` for each ring.
	let positions = $state<number[]>(Array(rings).fill(0));

	const guess = $derived(positions.map((p) => chars[p]).join(''));

	function rotate(ring: number, delta: number) {
		if (disabled) return;
		const n = chars.length;
		positions[ring] = (positions[ring] + delta + n) % n;
	}

	function charAt(ring: number, offset: number): string {
		const n = chars.length;
		return chars[(positions[ring] + offset + n) % n];
	}

	function onWheel(ring: number, e: WheelEvent) {
		e.preventDefault();
		rotate(ring, e.deltaY > 0 ? 1 : -1);
	}

	function onKey(ring: number, e: KeyboardEvent) {
		if (e.key === 'ArrowUp') {
			e.preventDefault();
			rotate(ring, -1);
		} else if (e.key === 'ArrowDown') {
			e.preventDefault();
			rotate(ring, 1);
		}
	}
</script>

<div class="cryptex" class:disabled>
	<div class="barrel">
		{#each Array(rings) as _, ring (ring)}
			<div
				class="ring"
				role="spinbutton"
				tabindex="0"
				aria-label={`Ring ${ring + 1}, current letter ${chars[positions[ring]]}`}
				aria-valuenow={positions[ring]}
				aria-valuetext={chars[positions[ring]]}
				onwheel={(e) => onWheel(ring, e)}
				onkeydown={(e) => onKey(ring, e)}
			>
				<button class="arrow up" aria-label="rotate up" onclick={() => rotate(ring, -1)}>▲</button>
				<div class="window">
					<span class="ghost">{charAt(ring, -1)}</span>
					<span class="active">{charAt(ring, 0)}</span>
					<span class="ghost">{charAt(ring, 1)}</span>
				</div>
				<button class="arrow down" aria-label="rotate down" onclick={() => rotate(ring, 1)}>▼</button>
			</div>
		{/each}
	</div>

	<button class="submit" {disabled} onclick={() => onsubmit?.(guess)}>Unlock</button>
</div>

<style>
	.cryptex {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 1.5rem;
		user-select: none;
	}
	.barrel {
		display: flex;
		gap: 0.4rem;
		padding: 1rem;
		background: linear-gradient(180deg, #4a3826, #2a1d12);
		border: 2px solid #6b4f33;
		border-radius: 14px;
		box-shadow:
			inset 0 2px 6px rgba(0, 0, 0, 0.6),
			0 6px 18px rgba(0, 0, 0, 0.4);
	}
	.ring {
		display: flex;
		flex-direction: column;
		align-items: center;
		outline: none;
	}
	.ring:focus-visible .window {
		box-shadow: 0 0 0 2px #d9a441;
	}
	.arrow {
		background: none;
		border: none;
		color: #c9a36a;
		cursor: pointer;
		font-size: 0.8rem;
		padding: 0.25rem;
		line-height: 1;
		transition: color 0.15s;
	}
	.arrow:hover {
		color: #f3d79a;
	}
	.window {
		display: flex;
		flex-direction: column;
		align-items: center;
		width: 2.6rem;
		height: 5.4rem;
		justify-content: center;
		background: linear-gradient(180deg, #1c140d, #3a2a1a, #1c140d);
		border-radius: 6px;
		overflow: hidden;
		font-family: 'Courier New', monospace;
		font-weight: 700;
	}
	.window .ghost {
		color: #6b5638;
		font-size: 1.1rem;
		opacity: 0.6;
	}
	.window .active {
		color: #f3d79a;
		font-size: 1.9rem;
		margin: 0.2rem 0;
		text-shadow: 0 1px 2px rgba(0, 0, 0, 0.8);
	}
	.submit {
		padding: 0.7rem 2.4rem;
		font-size: 1rem;
		font-weight: 600;
		letter-spacing: 0.05em;
		color: #2a1d12;
		background: linear-gradient(180deg, #f3d79a, #d9a441);
		border: none;
		border-radius: 8px;
		cursor: pointer;
		transition: filter 0.15s;
	}
	.submit:hover:not(:disabled) {
		filter: brightness(1.08);
	}
	.submit:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
	.disabled .barrel {
		opacity: 0.6;
	}
</style>
