<script lang="ts">
	import { usageTone } from '$lib/utils/status';

	interface Props {
		/** 0..100 */
		value: number;
		label?: string;
		/** Right-hand detail, e.g. "12.0 / 32.0 GB". */
		detail?: string;
		title?: string;
	}

	let { value, label, detail, title }: Props = $props();

	const barClass = { accent: 'bg-primary-500', warning: 'bg-warning-500', danger: 'bg-error-500', success: 'bg-success-500', neutral: 'bg-fg-subtle' };
	let pct = $derived(Math.max(0, Math.min(100, value || 0)));
</script>

<div {title}>
	{#if label || detail}
		<div class="flex justify-between gap-2 text-xs mb-1.5">
			<span class="text-fg-muted">{label}</span>
			<span class="text-fg-subtle tabular-nums truncate">{detail}</span>
		</div>
	{/if}
	<div class="rq-progress-track" role="progressbar" aria-valuenow={Math.round(pct)} aria-valuemin={0} aria-valuemax={100} aria-label={label}>
		<div class="rq-progress-bar {barClass[usageTone(pct)]}" style="width: {pct}%"></div>
	</div>
</div>
