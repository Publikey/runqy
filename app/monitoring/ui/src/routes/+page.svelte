<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { base } from '$app/paths';
	import { queuesStore, totalStats } from '$lib/stores/queues';
	import { workersStore, workerStats } from '$lib/stores/workers';
	import { settings } from '$lib/stores/settings';
	import QueueCard from '$lib/components/QueueCard.svelte';
	import QueueGroupCard from '$lib/components/QueueGroupCard.svelte';
	import StatCard from '$lib/components/StatCard.svelte';
	import ThroughputChart from '$lib/components/charts/ThroughputChart.svelte';
	import QueueSizesChart from '$lib/components/charts/QueueSizesChart.svelte';
	import { formatRelativeTime, truncateId } from '$lib/utils/format';
	import { groupQueues, hasMultiQueueGroups } from '$lib/utils/queueGrouping';

	// Create a set of server IDs that have active workers (processing tasks)
	let processingServerIds = $derived(
		new Set(
			$workersStore.servers
				.filter(s => s.active_workers && s.active_workers.length > 0)
				.map(s => s.id)
		)
	);

	// Check if a worker is processing by matching worker_id to server.id
	function isWorkerProcessing(workerId: string): boolean {
		return processingServerIds.has(workerId);
	}

	// Get worker status: 'processing' | 'idle' | 'stale' | 'bootstrapping' | 'stopped'
	function getWorkerStatus(worker: typeof $workersStore.workers[0]): string {
		if (worker.is_stale) return 'stale';
		if (worker.status === 'bootstrapping') return 'bootstrapping';
		if (worker.status === 'stopped') return 'stopped';
		if (worker.status === 'running' && isWorkerProcessing(worker.worker_id)) return 'processing';
		if (worker.status === 'running') return 'idle';
		return 'stopped'; // fallback for unknown statuses
	}

	// Parse queues string like "map[queue1:5 queue2:5]" to array of {name, priority}
	function parseQueues(queuesStr: string): { name: string; priority: number }[] {
		const match = queuesStr.match(/map\[(.*)\]/);
		if (!match) return [];
		return match[1].split(' ').map(q => {
			const [name, priorityStr] = q.split(':');
			return { name, priority: parseInt(priorityStr, 10) || 0 };
		}).filter(q => q.name);
	}

	let pollInterval: ReturnType<typeof setInterval> | null = null;
	let refreshing = $state(false);
	let groupQueuesEnabled = $state(true);

	// Grouped queues
	let queueGroups = $derived(groupQueues($queuesStore.queues));
	let showGroupToggle = $derived(hasMultiQueueGroups($queuesStore.queues));

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

	function navigateToQueue(qname: string) {
		goto(`${base}/queues/${encodeURIComponent(qname)}`);
	}
</script>

<svelte:head>
	<title>Dashboard - runqy Monitor</title>
</svelte:head>

<div class="rq-page space-y-8">
	<!-- Header -->
	<div class="flex items-center justify-between">
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
		<StatCard label="Total Queues" value={$totalStats.totalQueues} variant="primary" href="/queues" />
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

	<!-- Error State -->
	{#if $queuesStore.error}
		<div class="rq-card p-4">
			<p class="text-error-500">Failed to load queues: {$queuesStore.error}</p>
		</div>
	{/if}

	<!-- Queues Grid -->
	<div>
		<div class="flex items-center justify-between mb-4">
			<h2 class="rq-section-title">Queues</h2>
			{#if showGroupToggle}
				<button
					type="button"
					class="{groupQueuesEnabled ? 'rq-pill-active' : 'rq-pill'}"
					onclick={() => groupQueuesEnabled = !groupQueuesEnabled}
					title={groupQueuesEnabled ? 'Show individual queues' : 'Group sub-queues'}
				>
					<svg class="w-4 h-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						{#if groupQueuesEnabled}
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
						{:else}
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 10h16M4 14h16M4 18h16" />
						{/if}
					</svg>
					{groupQueuesEnabled ? 'Grouped' : 'Ungrouped'}
				</button>
			{/if}
		</div>

		{#if $queuesStore.loading && $queuesStore.queues.length === 0}
			<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
				{#each [1, 2, 3, 4, 5, 6] as i (i)}
					<div class="rq-card p-5">
						<div class="animate-pulse space-y-3">
							<div class="h-6 bg-surface-300 dark:bg-surface-600 rounded w-1/2"></div>
							<div class="grid grid-cols-3 gap-3">
								<div class="h-16 bg-surface-300 dark:bg-surface-600 rounded"></div>
								<div class="h-16 bg-surface-300 dark:bg-surface-600 rounded"></div>
								<div class="h-16 bg-surface-300 dark:bg-surface-600 rounded"></div>
							</div>
						</div>
					</div>
				{/each}
			</div>
		{:else if $queuesStore.queues.length === 0}
			<div class="rq-card rq-empty-state">
				<svg class="w-12 h-12 mx-auto text-surface-400 mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10"
					/>
				</svg>
				<p class="text-surface-500">No queues found</p>
				<p class="text-sm text-surface-400 mt-1">Queues will appear here when workers connect</p>
			</div>
		{:else if groupQueuesEnabled}
			<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
				{#each queueGroups as group (group.name)}
					<QueueGroupCard {group} workers={$workersStore.workers} onQueueClick={navigateToQueue} />
				{/each}
			</div>
		{:else}
			<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
				{#each $queuesStore.queues as queue (queue.queue)}
					<QueueCard {queue} workers={$workersStore.workers} onClick={() => navigateToQueue(queue.queue)} />
				{/each}
			</div>
		{/if}
	</div>

	<!-- Workers Overview -->
	{#if $workersStore.workers.length > 0}
		<div class="mt-8">
			<div class="flex items-center justify-between mb-4">
				<h2 class="rq-section-title">Workers Overview</h2>
				<a href="{base}/workers" class="text-primary-500 hover:text-primary-600 text-sm font-medium">
					View all →
				</a>
			</div>

			<!-- Worker Stats -->
			<div class="grid grid-cols-5 gap-4 mb-4">
				<div class="rq-card p-3 text-center">
					<div class="text-2xl font-bold text-primary-500">{$workerStats.processing}</div>
					<div class="text-xs text-surface-500">Processing</div>
				</div>
				<div class="rq-card p-3 text-center">
					<div class="text-2xl font-bold text-success-500">{$workerStats.idle}</div>
					<div class="text-xs text-surface-500">Idle</div>
				</div>
				<div class="rq-card p-3 text-center">
					<div class="text-2xl font-bold text-secondary-500">{$workerStats.bootstrapping}</div>
					<div class="text-xs text-surface-500">Bootstrapping</div>
				</div>
				<div class="rq-card p-3 text-center">
					<div class="text-2xl font-bold text-warning-500">{$workerStats.stale}</div>
					<div class="text-xs text-surface-500">Stale</div>
				</div>
				<div class="rq-card p-3 text-center">
					<div class="text-2xl font-bold text-surface-500">{$workerStats.stopped}</div>
					<div class="text-xs text-surface-500">Stopped</div>
				</div>
			</div>

			<!-- Worker List Table -->
			<div class="table-container">
				<table class="rq-table">
					<thead>
						<tr>
							<th>Worker ID</th>
							<th>Status</th>
							<th>Queues</th>
							<th>Last Beat</th>
						</tr>
					</thead>
					<tbody>
						{#each $workersStore.workers.slice(0, 8) as worker (worker.worker_id)}
							<tr>
								<td class="font-mono text-sm" title={worker.worker_id}>{truncateId(worker.worker_id, 24)}</td>
								<td>
									{#if getWorkerStatus(worker) === 'processing'}
										<span class="badge preset-filled-primary-500 text-xs">Processing</span>
									{:else if getWorkerStatus(worker) === 'idle'}
										<span class="badge preset-filled-success-500 text-xs">Idle</span>
									{:else if getWorkerStatus(worker) === 'bootstrapping'}
										<span class="badge preset-filled-secondary-500 text-xs">Bootstrapping</span>
									{:else if getWorkerStatus(worker) === 'stale'}
										<span class="badge preset-filled-warning-500 text-xs">Stale</span>
									{:else}
										<span class="badge preset-filled-surface-500 text-xs">Stopped</span>
									{/if}
								</td>
								<td>
									<div class="flex flex-wrap gap-1">
										{#each parseQueues(worker.queues) as queue (queue.name)}
											<span class="badge preset-outlined-primary-500 text-xs">
												{queue.name}
												{#if queue.priority === 0 && getWorkerStatus(worker) === 'bootstrapping'}
													<svg class="w-3 h-3 ml-1 animate-spin inline-block" fill="none" viewBox="0 0 24 24">
														<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
														<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
													</svg>
												{/if}
											</span>
										{/each}
									</div>
								</td>
								<td class="text-surface-500 text-sm">{formatRelativeTime(new Date(worker.last_beat * 1000))}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>

			{#if $workersStore.workers.length > 8}
				<div class="text-center mt-4">
					<a href="{base}/workers" class="text-primary-500 hover:text-primary-600 text-sm font-medium">
						+{$workersStore.workers.length - 8} more workers →
					</a>
				</div>
			{/if}
		</div>
	{:else}
		<div class="mt-8">
			<h2 class="rq-section-title mb-4">Workers Overview</h2>
			<div class="rq-card rq-empty-state">
				<svg class="w-12 h-12 mx-auto text-surface-400 mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M9 3v2m6-2v2M9 19v2m6-2v2M5 9H3m2 6H3m18-6h-2m2 6h-2M7 19h10a2 2 0 002-2V7a2 2 0 00-2-2H7a2 2 0 00-2 2v10a2 2 0 002 2zM9 9h6v6H9V9z"
					/>
				</svg>
				<p class="text-surface-500">No workers found</p>
				<p class="text-sm text-surface-400 mt-1">Workers will appear here when they connect</p>
			</div>
		</div>
	{/if}
</div>
