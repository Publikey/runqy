<script lang="ts">
	import { workerQueueNames } from '$lib/utils/workerQueues';
	import { onMount, onDestroy } from 'svelte';
	import { base } from '$app/paths';
	import { queuesStore, totalStats } from '$lib/stores/queues';
	import { workersStore, workerStats } from '$lib/stores/workers';
	import { settings } from '$lib/stores/settings';
	import StatCard from '$lib/components/StatCard.svelte';
	import ThroughputChart from '$lib/components/charts/ThroughputChart.svelte';
	import QueueSizesChart from '$lib/components/charts/QueueSizesChart.svelte';
	import { formatDuration, formatNumber, formatRelativeTime } from '$lib/utils/format';
	import { groupQueues, type QueueGroup } from '$lib/utils/queueGrouping';

	let pollInterval: ReturnType<typeof setInterval> | null = null;
	let refreshing = $state(false);

	let queueGroups = $derived(groupQueues($queuesStore.queues));

	// Parent queues whose sub-queues no worker listens on.
	function hasWorkers(group: QueueGroup): boolean {
		const names = new Set(group.queues.map((q) => q.queue));
		return $workersStore.workers.some((w) => workerQueueNames(w.queues).some((n) => names.has(n)));
	}

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

	async function handleRefresh() {
		refreshing = true;
		await loadData();
		setTimeout(() => { refreshing = false; }, 500);
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

<div class="rq-page space-y-8">
	<!-- Header -->
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<h1 class="rq-page-title">Dashboard</h1>
			<p class="text-surface-500">
				{#if $queuesStore.lastUpdated}
					Last updated {formatRelativeTime($queuesStore.lastUpdated)}
				{:else}
					Loading...
				{/if}
			</p>
		</div>
		<button type="button" class="rq-btn-ghost {refreshing ? 'refresh-spinning' : ''}" onclick={handleRefresh}>
			<svg class="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path
					stroke-linecap="round"
					stroke-linejoin="round"
					stroke-width="2"
					d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
				/>
			</svg>
			Refresh
		</button>
	</div>

	<!-- Stats Overview -->
	<div class="grid grid-cols-2 md:grid-cols-4 lg:grid-cols-6 gap-4">
		<StatCard label="Queues" value={queueGroups.length} variant="primary" href="/queues" />
		<StatCard label="Pending" value={$totalStats.totalPending} variant="warning" />
		<StatCard label="Active" value={$totalStats.totalActive} variant="success" />
		<StatCard label="Processed" value={$totalStats.totalProcessed} variant="default" />
		<StatCard label="Failed" value={$totalStats.totalFailed} variant="error" />
		<StatCard label="Workers" value={$workerStats.total} variant="primary" href="/workers" />
	</div>

	<!-- Charts Section -->
	<div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
		<ThroughputChart height={280} />
		<QueueSizesChart height={280} maxQueues={6} />
	</div>

	{#if $queuesStore.error}
		<div class="rq-card p-4">
			<p class="text-error-500">Failed to load queues: {$queuesStore.error}</p>
		</div>
	{/if}

	<div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
		<!-- Queues Overview -->
		<section class="rq-card p-5 space-y-5">
			<div class="flex items-center justify-between">
				<h2 class="rq-section-title">Queues</h2>
				<a href="{base}/queues" class="text-primary-500 hover:text-primary-600 text-sm font-medium">View all →</a>
			</div>

			<div class="grid grid-cols-2 sm:grid-cols-4 gap-3">
				<div class="rq-metric-box text-center">
					<div class="text-xl font-bold">{queueGroups.length}</div>
					<div class="text-xs text-surface-500">Queues</div>
				</div>
				<div class="rq-metric-box text-center">
					<div class="text-xl font-bold">{$queuesStore.queues.length}</div>
					<div class="text-xs text-surface-500">Sub-queues</div>
				</div>
				<div class="rq-metric-box text-center">
					<div class="text-xl font-bold {pausedSubQueues > 0 ? 'text-warning-500' : ''}">{pausedSubQueues}</div>
					<div class="text-xs text-surface-500">Paused</div>
				</div>
				<div class="rq-metric-box text-center">
					<div class="text-xl font-bold {groupsWithoutWorkers.length > 0 ? 'text-warning-500' : ''}">{groupsWithoutWorkers.length}</div>
					<div class="text-xs text-surface-500">Without worker</div>
				</div>
			</div>

			{#if $queuesStore.queues.length === 0}
				<p class="text-sm text-surface-400">
					{$queuesStore.loading ? 'Loading…' : 'No queues yet. They appear once configured.'}
				</p>
			{:else}
				<div>
					<h3 class="text-xs font-medium uppercase tracking-wider text-surface-400 mb-2">Needs attention</h3>
					{#if attention.length === 0}
						<p class="text-sm text-success-500">Every queue has a worker, none is paused and nothing failed.</p>
					{:else}
						<ul class="space-y-1">
							{#each attention as a (a.group.name)}
								<li>
									<a href={queueHref(a.group)} class="flex flex-wrap items-center justify-between gap-2 rounded px-2 py-1.5 hover:bg-surface-100-900">
										<span class="font-mono text-sm truncate">{a.group.name}</span>
										<span class="flex flex-wrap gap-1">
											{#each a.issues as issue (issue)}
												<span class="badge preset-outlined-warning-500 text-xs">{issue}</span>
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
						<div class="flex items-center justify-between mb-2 px-2">
							<h3 class="text-xs font-medium uppercase tracking-wider text-surface-400">Busiest</h3>
							<span class="flex items-center gap-3 text-[0.65rem] uppercase tracking-wider text-surface-400">
								<span class="text-warning-500">Pending</span>
								<span class="text-success-500">Active</span>
								<span>Latency</span>
							</span>
						</div>
						<ul class="space-y-1">
							{#each busiest as g (g.name)}
								<li>
									<a href={queueHref(g)} class="flex items-center justify-between gap-3 rounded px-2 py-1.5 hover:bg-surface-100-900 text-sm">
										<span class="font-mono truncate">{g.name}</span>
										<span class="flex items-center gap-3 font-mono shrink-0">
											<span class="text-warning-500" title="Pending">{formatNumber(g.pending)}</span>
											<span class="text-success-500" title="Active">{formatNumber(g.active)}</span>
											<span class="text-surface-500" title="Highest latency">{formatDuration(g.latency_msec)}</span>
										</span>
									</a>
								</li>
							{/each}
						</ul>
					</div>
				{/if}
			{/if}
		</section>

		<!-- Workers Overview -->
		<section class="rq-card p-5 space-y-5">
			<div class="flex items-center justify-between">
				<h2 class="rq-section-title">Workers</h2>
				<a href="{base}/workers" class="text-primary-500 hover:text-primary-600 text-sm font-medium">View all →</a>
			</div>

			{#if $workersStore.workers.length === 0}
				<p class="text-sm text-surface-400">
					{$workersStore.loading ? 'Loading…' : 'No workers connected. They appear here when they register.'}
				</p>
			{:else}
				<div class="grid grid-cols-2 sm:grid-cols-3 gap-3">
					<div class="rq-metric-box text-center">
						<div class="text-xl font-bold">{$workerStats.total}</div>
						<div class="text-xs text-surface-500">Total</div>
					</div>
					<div class="rq-metric-box text-center">
						<div class="text-xl font-bold text-primary-500">{$workerStats.processing}</div>
						<div class="text-xs text-surface-500">Processing</div>
					</div>
					<div class="rq-metric-box text-center">
						<div class="text-xl font-bold text-success-500">{$workerStats.idle}</div>
						<div class="text-xs text-surface-500">Idle</div>
					</div>
					<div class="rq-metric-box text-center">
						<div class="text-xl font-bold text-secondary-500">{$workerStats.bootstrapping}</div>
						<div class="text-xs text-surface-500">Bootstrapping</div>
					</div>
					<div class="rq-metric-box text-center">
						<div class="text-xl font-bold {$workerStats.stale > 0 ? 'text-warning-500' : ''}">{$workerStats.stale}</div>
						<div class="text-xs text-surface-500">Stale</div>
					</div>
					<div class="rq-metric-box text-center">
						<div class="text-xl font-bold text-surface-500">{$workerStats.stopped}</div>
						<div class="text-xs text-surface-500">Stopped</div>
					</div>
				</div>
			{/if}
		</section>
	</div>
</div>
