<script lang="ts" module>
	import type { Tone } from '$lib/utils/status';

	export interface Stat {
		label: string;
		value: number | string;
		/** Colour only for anomalies (e.g. failed > 0 → danger). Zero values are dimmed. */
		tone?: Tone | 'default';
		hint?: string;
		/** App-relative link. */
		href?: string;
	}
</script>

<script lang="ts">
	// A row of figures separated by hairlines, without a box of its own: for numbers that sit
	// inside a Panel (no box-in-a-box) or as a page-level activity strip (`card`).
	import { base } from '$app/paths';
	import { formatNumber } from '$lib/utils/format';

	interface Props {
		items: Stat[];
		/** Columns on phones (the strip wraps); all items share one row from md up. */
		mobileColumns?: 2 | 3;
		/** Give the strip its own card (page-level strip). */
		card?: boolean;
	}

	let { items, mobileColumns = 3, card = false }: Props = $props();

	const toneClass: Record<string, string> = {
		default: 'text-fg',
		neutral: 'text-fg-muted',
		accent: 'text-primary-300',
		success: 'text-success-300',
		warning: 'text-warning-300',
		danger: 'text-error-300'
	};

	function valueClass(s: Stat): string {
		if (s.value === 0 || s.value === '0') return 'text-fg-faint';
		return toneClass[s.tone ?? 'default'];
	}

	const cols = { 2: 'grid-cols-2', 3: 'grid-cols-3' };
</script>

<div
	class="grid {cols[mobileColumns]} md:flex md:flex-wrap {card ? 'rq-card' : ''} rq-stat-strip"
	style="--rq-strip-n: {items.length}"
>
	{#each items as s (s.label)}
		<svelte:element
			this={s.href ? 'a' : 'div'}
			href={s.href ? base + s.href : undefined}
			class="rq-stat {s.href ? 'rq-stat-link' : ''}"
		>
			<div class="text-2xs md:text-xs text-fg-subtle truncate">{s.label}</div>
			<div class="mt-0.5 text-base md:text-lg font-semibold tracking-tight tabular-nums {valueClass(s)}">
				{typeof s.value === 'number' ? formatNumber(s.value) : s.value}
			</div>
			{#if s.hint}<div class="text-2xs text-fg-faint truncate">{s.hint}</div>{/if}
		</svelte:element>
	{/each}
</div>
