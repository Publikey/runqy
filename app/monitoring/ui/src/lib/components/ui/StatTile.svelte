<script lang="ts">
	import { base } from '$app/paths';
	import { formatNumber } from '$lib/utils/format';
	import type { Tone } from '$lib/utils/status';

	interface Props {
		label: string;
		value: number | string;
		/** Colours the value: use for data that is good/bad (failed > 0 → danger). */
		tone?: Tone | 'default';
		hint?: string;
		/** App-relative link. */
		href?: string;
		/** Compact tile inside a panel (no card chrome). */
		inset?: boolean;
	}

	let { label, value, tone = 'default', hint, href, inset = false }: Props = $props();

	const toneClass: Record<string, string> = {
		default: 'text-fg',
		neutral: 'text-fg-muted',
		accent: 'text-primary-300',
		success: 'text-success-300',
		warning: 'text-warning-300',
		danger: 'text-error-300'
	};

	let display = $derived(typeof value === 'number' ? formatNumber(value) : value);
	let chrome = $derived(inset ? 'rq-metric-box' : 'rq-card px-4 py-3.5');
</script>

<svelte:element
	this={href ? 'a' : 'div'}
	href={href ? base + href : undefined}
	class="{chrome} block min-w-0 {href ? 'rq-card-interactive' : ''}"
>
	<div class="text-ui text-fg-muted truncate">{label}</div>
	<div class="mt-1 text-xl font-semibold tracking-tight tabular-nums {toneClass[tone]}">{display}</div>
	{#if hint}<div class="mt-0.5 text-xs text-fg-subtle truncate">{hint}</div>{/if}
</svelte:element>
