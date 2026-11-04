<script lang="ts">
	import { workerQueueNames } from '$lib/utils/workerQueues';
	import { onMount, onDestroy } from 'svelte';
	import { base } from '$app/paths';
	import { queuesStore, totalStats } from '$lib/stores/queues';
	import { workersStore, workerStats } from '$lib/stores/workers';
	import { settings } from '$lib/stores/settings';
	import { Callout, Badge, MoreLink, PageHeader, Panel, StatStrip } from '$lib/components/ui';
	import { todayStats } from '$lib/stores/metrics';
	import { countClass } from '$lib/utils/status';
	import ThroughputChart from '$lib/components/charts/ThroughputChart.svelte';
	import QueueSizesChart from '$lib/components/charts/QueueSizesChart.svelte';
	import { formatDuration, formatNumber } from '$lib/utils/format';
	import { groupQueues, type QueueGroup } from '$lib/utils/queueGrouping';

	// Shorter charts on phones: 280px of chart for a handful of points is a full screen.
	let chartHeight = $state(280);
	$effect(() => {
		const mq = window.matchMedia('(max-width: 767px)');
		const update = () => (chartHeight = mq.matches ? 180 : 280);
		update();
		mq.addEventListener('change', update);
		return () => mq.removeEventListener('change', update);
	});

	let pollInterval: ReturnType<typeof setInterval> | null = null;

	let queueGroups = $derived(groupQueues($queuesStore.queues));

	// Parent queues whose sub-queues no worker listens on.
	function hasWorkers(group: QueueGroup): boolean {
		const names = new Set(group.queues.map((q) => q.queue));
		return $workersStore.workers.some((w) => workerQueueNames(w.queues).some((n) => names.has(n)));
	}

	// Oldest waiting task across all queues.
	let maxLatency = $derived(Math.max(0, ...$queuesStore.queues.map((q) => q.latency_msec || 0)));

	let pausedSubQueues = $derived($queuesStore.queues.filter((q) => q.paused).length);
	let groupsWithoutWorkers = $derived(queueGroups.filter((g) => !hasWorkers(g)));

	interface Attention {
		group: QueueGroup;
		issues: string[];
	}

	// Queues worth a look: waiting work with no worker, paused sub-queues, failures.
	let attention = $derived.by((): Attention[] => {
		const out: Attention[] = [];
		for (const g of queueGroups) {
			const issues: string[] = [];
			if (!hasWorkers(g)) {
				issues.push(g.pending > 0 ? `${formatNumber(g.pending)} pending, no worker` : 'no worker');
			}
			const paused = g.queues.filter((q) => q.paused).length;
			if (paused > 0) {
				issues.push(paused === g.queues.length ? 'paused' : `${paused}/${g.queues.length} paused`);
			}
			if (g.failed > 0) issues.push(`${formatNumber(g.failed)} failed`);
			if (issues.length) out.push({ group: g, issues });
		}
		return out.sort((a, b) => b.group.pending - a.group.pending).slice(0, 6);
	});

	// Busiest parent queues by waiting + running work.
	let busiest = $derived(
		[...queueGroups]
			.filter((g) => g.pending + g.active > 0)
			.sort((a, b) => b.pending + b.active - (a.pending + a.active))
			.slice(0, 5)
	);

	function queueHref(g: QueueGroup): string {
		return g.queues.length === 1
			? `${base}/queues/${encodeURIComponent(g.queues[0].queue)}`
			: `${base}/queues?q=${encodeURIComponent(g.name)}`;
	}

	async function loadData() {
		await Promise.all([queuesStore.fetch(), workersStore.fetch()]);
	}

	onMount(() => {
		loadData();
		pollInterval = setInterval(loadData, $settings.pollInterval * 1000);
	});

	onDestroy(() => {
		if (pollInterval) clearInterval(pollInterval);
	});
</script>

<svelte:head>
	<title>Dashboard - runqy Monitor</title>
</svelte:head>

<div class="rq-page space-y-4 md:space-y-6">
	<PageHeader title="Dashboard" subtitle={$queuesStore.lastUpdated ? undefined : 'Loading…'} onrefresh={loadData} />

	<StatStrip
		card
		items={[
			{ label: 'Pending', value: $totalStats.totalPending },
			{ label: 'Active', value: $totalStats.totalActive },
			{ label: 'Processed today', value: $todayStats.processed },
			{ label: 'Failed today', value: $todayStats.failed, tone: 'danger' },
			{ label: 'Max latency', value: maxLatency > 0 ? formatDuration(maxLatency) : 0 }
		]}
	/>

	<div class="grid grid-cols-1 xl:grid-cols-2 gap-6">
		<ThroughputChart height={chartHeight} />
		<QueueSizesChart height={chartHeight} maxQueues={6} />
	</div>

	{#if $queuesStore.error}
		<Callout>Failed to load queues: {$queuesStore.error}</Callout>
	{/if}

	<div class="grid grid-cols-1 xl:grid-cols-2 gap-6">
		<Panel title="Queues" class="min-w-0">
			{#snippet actions()}
				<MoreLink href="/queues" label="View all" />
			{/snippet}

			<div class="space-y-6">
				<StatStrip
					mobileColumns={2}
					items={[
						{ label: 'Queues', value: queueGroups.length },
						{ label: 'Sub-queues', value: $queuesStore.queues.length },
						{ label: 'Paused', value: pausedSubQueues, tone: 'warning' },
						{ label: 'Without worker', value: groupsWithoutWorkers.length, tone: 'warning' }
					]}
				/>

				{#if $queuesStore.queues.length === 0}
					<p class="text-ui text-fg-subtle">
						{$queuesStore.loading ? 'Loading…' : 'No queues yet. They appear once configured.'}
					</p>
				{:else}
					<div>
						<h3 class="text-xs font-medium text-fg-subtle mb-2">Needs attention</h3>
						{#if attention.length === 0}
							<p class="text-ui text-fg-muted">Every queue has a worker, none is paused and nothing failed.</p>
						{:else}
							<ul class="-mx-2">
								{#each attention as a (a.group.name)}
									<li>
										<a
											href={queueHref(a.group)}
											class="flex flex-wrap items-center justify-between gap-2 rounded-md px-2 py-1.5 hover:bg-hover transition-colors"
										>
											<span class="text-ui font-medium text-fg truncate min-w-0">{a.group.name}</span>
											<span class="flex flex-wrap gap-1">
												{#each a.issues as issue (issue)}
													<Badge tone={issue.endsWith(' failed') ? 'danger' : 'warning'}>{issue}</Badge>
												{/each}
											</span>
										</a>
									</li>
								{/each}
							</ul>
						{/if}
					</div>

					{#if busiest.length > 0}
						<div>
							<div class="flex items-center justify-between gap-3 mb-2">
								<h3 class="text-xs font-medium text-fg-subtle">Busiest</h3>
								<span class="flex items-center gap-3 text-2xs text-fg-subtle">
									<span class="w-14 text-right">Pending</span>
									<span class="w-14 text-right">Active</span>
									<span class="w-16 text-right">Latency</span>
								</span>
							</div>
							<ul class="-mx-2">
								{#each busiest as g (g.name)}
									<li>
										<a
											href={queueHref(g)}
											class="flex items-center justify-between gap-3 rounded-md px-2 py-1.5 hover:bg-hover transition-colors text-ui"
										>
											<span class="font-medium text-fg truncate min-w-0">{g.name}</span>
											<span class="flex items-center gap-3 tabular-nums shrink-0">
												<span class="w-14 text-right {countClass(g.pending)}" title="Pending"
													>{formatNumber(g.pending)}</span
												>
												<span class="w-14 text-right {countClass(g.active)}" title="Active"
													>{formatNumber(g.active)}</span
												>
												<span class="w-16 text-right text-fg-subtle" title="Highest latency">{formatDuration(g.latency_msec)}</span>
											</span>
										</a>
									</li>
								{/each}
							</ul>
						</div>
					{/if}
				{/if}
			</div>
		</Panel>

		<Panel title="Workers" class="min-w-0">
			{#snippet actions()}
				<MoreLink href="/workers" label="View all" />
			{/snippet}

			{#if $workersStore.workers.length === 0}
				<p class="text-ui text-fg-subtle">
					{$workersStore.loading ? 'Loading…' : 'No workers connected. They appear here when they register.'}
				</p>
			{:else}
				<StatStrip
					items={[
						{ label: 'Total', value: $workerStats.total },
						{ label: 'Processing', value: $workerStats.processing },
						{ label: 'Idle', value: $workerStats.idle },
						{ label: 'Bootstrapping', value: $workerStats.bootstrapping },
						{ label: 'Stale', value: $workerStats.stale, tone: 'warning' },
						{ label: 'Stopped', value: $workerStats.stopped }
					]}
				/>
			{/if}
		</Panel>
	</div>
</div>
