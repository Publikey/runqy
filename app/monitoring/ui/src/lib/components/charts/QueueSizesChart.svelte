<script lang="ts">
	import { queuesStore } from '$lib/stores/queues';
	import { base } from '$app/paths';
	import { Panel, Shimmer } from '$lib/components/ui';

	interface Props {
		/** Height of the loading placeholder. */
		height?: number;
		maxQueues?: number;
	}

	let { height = 250, maxQueues = 8 }: Props = $props();

	// Pending is normal work: neutral grey. Retry is the anomaly.
	const stateColors = {
		pending: 'bg-fg-muted',
		active: 'bg-primary-500',
		retry: 'bg-warning-500'
	};

	const states = ['pending', 'active', 'retry'] as const;

	const stateLabels = {
		pending: 'Pending',
		active: 'Active',
		retry: 'Retry'
	};

	interface QueueRow {
		queue: string;
		pending: number;
		active: number;
		retry: number;
		total: number;
	}

	// The busiest queues that hold work, one row each. Queue names are too long for
	// column labels at any width, so each row carries the full name and a horizontal bar.
	let rows = $derived.by((): QueueRow[] =>
		$queuesStore.queues
			.map((q) => ({
				queue: q.queue,
				pending: q.pending || 0,
				active: q.active || 0,
				retry: q.retry || 0,
				total: (q.pending || 0) + (q.active || 0) + (q.retry || 0)
			}))
			.filter((r) => r.total > 0)
			.sort((a, b) => b.total - a.total)
			.slice(0, maxQueues)
	);

	let busyQueues = $derived(
		$queuesStore.queues.filter((q) => (q.pending || 0) + (q.active || 0) + (q.retry || 0) > 0).length
	);

	// Bars are scaled to the busiest queue.
	let maxTotal = $derived(Math.max(1, ...rows.map((r) => r.total)));

	/** Parent queue and sub-queue suffix (".priority"), shown apart so the suffix survives truncation. */
	function splitQueueName(name: string): { parent: string; sub: string } {
		const dot = name.indexOf('.');
		return dot >= 0 ? { parent: name.slice(0, dot), sub: name.slice(dot) } : { parent: name, sub: '' };
	}

	function formatNumber(val: number): string {
		if (val >= 1000000) return (val / 1000000).toFixed(1) + 'M';
		if (val >= 1000) return (val / 1000).toFixed(1) + 'K';
		return val.toString();
	}
</script>

<Panel title="Live Queue Sizes" class="min-w-0" bodyClass="min-w-0">
	{#snippet actions()}
		{#if busyQueues > maxQueues}
			<span class="text-xs text-fg-subtle">Top {maxQueues} of {busyQueues} busy queues</span>
		{/if}
	{/snippet}

	{#if $queuesStore.loading && $queuesStore.queues.length === 0}
		<div style="height: {height}px">
			<Shimmer class="h-full w-full" />
		</div>
	{:else if $queuesStore.queues.length === 0}
		<div class="flex h-20 items-center justify-center">
			<p class="text-ui text-fg-subtle">No queues available</p>
		</div>
	{:else if rows.length === 0}
		<div class="flex h-20 items-center justify-center">
			<p class="text-ui text-fg-subtle">All queues are empty</p>
		</div>
	{:else}
		<ul class="-my-1 divide-y divide-line-subtle">
			{#each rows as row (row.queue)}
				{@const name = splitQueueName(row.queue)}
				<li>
					<a href="{base}/queues/{encodeURIComponent(row.queue)}" class="block py-2 hover:bg-hover transition-colors">
						<div class="flex items-baseline gap-3 min-w-0">
							<span class="flex min-w-0 flex-1 text-ui" title={row.queue}>
								<span class="truncate font-medium text-fg">{name.parent}</span>
								<span class="shrink-0 text-fg-subtle">{name.sub}</span>
							</span>
							<span class="flex shrink-0 items-center gap-2.5 text-xs tabular-nums text-fg-muted">
								{#each states as state (state)}
									{#if row[state] > 0}
										<span class="flex items-center gap-1" title={stateLabels[state]}>
											<span class="h-2 w-2 rounded-full {stateColors[state]}"></span>
											{formatNumber(row[state])}
											<span class="sr-only">{stateLabels[state]}</span>
										</span>
									{/if}
								{/each}
							</span>
						</div>
						<div class="mt-1.5 flex h-1.5 gap-px overflow-hidden rounded-full bg-raised">
							{#each states as state (state)}
								{#if row[state] > 0}
									<div class="{stateColors[state]} min-w-[3px]" style="width: {(row[state] / maxTotal) * 100}%"></div>
								{/if}
							{/each}
						</div>
					</a>
				</li>
			{/each}
		</ul>

		<div class="flex items-center justify-center gap-4 mt-3 flex-wrap text-xs text-fg-muted">
			{#each states as state (state)}
				<span class="flex items-center gap-1.5">
					<span class="h-2 w-2 rounded-full {stateColors[state]}"></span>
					{stateLabels[state]}
				</span>
			{/each}
		</div>
	{/if}
</Panel>
