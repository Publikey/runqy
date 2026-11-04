<script lang="ts">
	// Global freshness indicator: pages refresh themselves on the poll interval, so instead
	// of a Refresh button per page this says whether data is live, and refreshes on click.
	import { live, requestRefresh } from '$lib/stores/live';
	import { settings } from '$lib/stores/settings';

	let { compact = false }: { compact?: boolean } = $props();

	let now = $state(Date.now());
	$effect(() => {
		const t = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(t);
	});

	let age = $derived($live.lastOk ? Math.max(0, Math.round((now - $live.lastOk) / 1000)) : null);
	// Stale: no answer for three poll intervals (the page may have stopped polling).
	let stale = $derived(age !== null && age > Math.max(15, $settings.pollInterval * 3));
	let status = $derived<'live' | 'failing' | 'stale' | 'idle'>(
		$live.failing ? 'failing' : age === null ? 'idle' : stale ? 'stale' : 'live'
	);

	function fmtAge(s: number): string {
		if (s < 60) return `${s}s`;
		if (s < 3600) return `${Math.floor(s / 60)}m`;
		return `${Math.floor(s / 3600)}h`;
	}

	let label = $derived(
		status === 'failing' ? 'Reconnecting…' : status === 'stale' ? 'Stale' : status === 'idle' ? 'Connecting…' : 'Live'
	);
	let title = $derived(
		status === 'failing'
			? 'The server does not answer. Click to retry.'
			: age !== null
				? `Last update ${fmtAge(age)} ago. Click to refresh now.`
				: 'Click to refresh now.'
	);
</script>

<button type="button" class="rq-live rq-live-{status}" {title} aria-label="{label}. {title}" onclick={requestRefresh}>
	<span class="rq-live-dot" aria-hidden="true"></span>
	<span>{label}</span>
	{#if !compact && status === 'live' && age !== null}
		<span class="text-fg-faint tabular-nums">{fmtAge(age)}</span>
	{/if}
</button>
