<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { StatusView, Tone } from '$lib/utils/status';

	interface Props {
		/** A status from $lib/utils/status (sets label and tone). */
		status?: StatusView;
		tone?: Tone | 'outline';
		/** Leading dot; on by default for statuses. */
		dot?: boolean;
		title?: string;
		class?: string;
		children?: Snippet;
	}

	let { status, tone, dot, title, class: klass = '', children }: Props = $props();

	let t = $derived(tone ?? status?.tone ?? 'neutral');
	let showDot = $derived(dot ?? (status !== undefined && t !== 'outline'));
</script>

<span class="rq-badge rq-badge-{t} {klass}" {title}>
	{#if showDot}<span class="rq-badge-dot" aria-hidden="true"></span>{/if}
	{#if children}{@render children()}{:else}{status?.label}{/if}
</span>
