<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { base } from '$app/paths';
	import { workersStore, workerStats } from '$lib/stores/workers';
	import { settings } from '$lib/stores/settings';
	import WorkerCard from '$lib/components/WorkerCard.svelte';
	import { formatRelativeTime } from '$lib/utils/format';

	let pollInterval: ReturnType<typeof setInterval> | null = null;
	let viewMode = $state<'cards' | 'table'>('cards');
	let statusFilter = $state<string | null>(null);
	let refreshing = $state(false);

	// Create a set of server IDs that have active workers (processing tasks)
	let processingServerIds = $derived(
		new Set(
			$workersStore.servers
				.filter(s => s.active_workers && s.active_workers.length > 0)
				.map(s => s.id)
		)
	);

	// Check if a worker is processing
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

	function formatBytes(bytes: number): string {
		if (bytes < 1024) return bytes + ' B';
		if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(0) + ' KB';
		if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
		return (bytes / (1024 * 1024 * 1024)).toFixed(1) + ' GB';
	}

	function barColor(percent: number): string {
		if (percent >= 90) return 'bg-error-500';
		if (percent >= 70) return 'bg-warning-500';
		return 'bg-success-500';
	}

	function navigateToWorker(workerId: string) {
		goto(`${base}/workers/${encodeURIComponent(workerId)}`);
	}

	let filteredWorkers = $derived(
		$workersStore.workers.filter((w) => {
			const status = getWorkerStatus(w);
			if (statusFilter === 'processing' && status !== 'processing') return false;
			if (statusFilter === 'idle' && status !== 'idle') return false;
			if (statusFilter === 'stale' && status !== 'stale') return false;
			if (statusFilter === 'bootstrapping' && status !== 'bootstrapping') return false;
			if (statusFilter === 'stopped' && status !== 'stopped') return false;
			return true;
		})
	);

	async function loadData() {
		await workersStore.fetch();
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
	<title>Workers - runqy Monitor</title>
</svelte:head>

<div class="rq-page space-y-8">
	<!-- Header -->
	<div class="flex items-center justify-between">
		<div>
			<h1 class="rq-page-title">Workers</h1>
			<p class="text-surface-500">
				{filteredWorkers.length} worker{filteredWorkers.length !== 1 ? 's' : ''}
				{#if $workersStore.lastUpdated}
					&middot; Updated {formatRelativeTime($workersStore.lastUpdated)}
				{/if}
			</p>
		</div>
		<div class="flex items-center gap-2">
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
	</div>

	<!-- Stats -->
	<div class="grid grid-cols-2 md:grid-cols-6 gap-4">
		<div class="rq-card p-4 text-center">
			<div class="text-2xl font-semibold tracking-tight">{$workerStats.total}</div>
			<div class="text-xs font-medium uppercase tracking-wider text-surface-400">Total Workers</div>
		</div>
		<div class="rq-card p-4 text-center">
			<div class="text-2xl font-semibold tracking-tight text-primary-500">{$workerStats.processing}</div>
			<div class="text-xs font-medium uppercase tracking-wider text-surface-400">Processing</div>
		</div>
		<div class="rq-card p-4 text-center">
			<div class="text-2xl font-semibold tracking-tight text-success-500">{$workerStats.idle}</div>
			<div class="text-xs font-medium uppercase tracking-wider text-surface-400">Idle</div>
		</div>
		<div class="rq-card p-4 text-center">
			<div class="text-2xl font-semibold tracking-tight text-secondary-500">{$workerStats.bootstrapping}</div>
			<div class="text-xs font-medium uppercase tracking-wider text-surface-400">Bootstrapping</div>
		</div>
		<div class="rq-card p-4 text-center">
			<div class="text-2xl font-semibold tracking-tight text-warning-500">{$workerStats.stale}</div>
			<div class="text-xs font-medium uppercase tracking-wider text-surface-400">Stale</div>
		</div>
		<div class="rq-card p-4 text-center">
			<div class="text-2xl font-semibold tracking-tight text-surface-500">{$workerStats.stopped}</div>
			<div class="text-xs font-medium uppercase tracking-wider text-surface-400">Stopped</div>
		</div>
	</div>

	<!-- Filters -->
	<div class="flex items-center justify-between">
		<div class="flex items-center gap-2 flex-wrap">
			<button
				type="button"
				class="{statusFilter === null ? 'rq-pill-active' : 'rq-pill'}"
				onclick={() => (statusFilter = null)}
			>
				All
			</button>
			<button
				type="button"
				class="{statusFilter === 'processing' ? 'rq-pill-active' : 'rq-pill'}"
				onclick={() => (statusFilter = 'processing')}
			>
				Processing
			</button>
			<button
				type="button"
				class="{statusFilter === 'idle' ? 'rq-pill-active' : 'rq-pill'}"
				onclick={() => (statusFilter = 'idle')}
			>
				Idle
			</button>
			<button
				type="button"
				class="{statusFilter === 'bootstrapping' ? 'rq-pill-active' : 'rq-pill'}"
				onclick={() => (statusFilter = 'bootstrapping')}
			>
				Bootstrapping
			</button>
			<button
				type="button"
				class="{statusFilter === 'stale' ? 'rq-pill-active' : 'rq-pill'}"
				onclick={() => (statusFilter = 'stale')}
			>
				Stale
			</button>
			<button
				type="button"
				class="{statusFilter === 'stopped' ? 'rq-pill-active' : 'rq-pill'}"
				onclick={() => (statusFilter = 'stopped')}
			>
				Stopped
			</button>
		</div>

		<div class="flex items-center gap-1 bg-surface-200 dark:bg-surface-700 rounded-lg p-1">
			<button
				type="button"
				class="p-2 rounded {viewMode === 'cards' ? 'bg-white dark:bg-surface-600 shadow-sm' : ''}"
				onclick={() => (viewMode = 'cards')}
				title="Card view"
			>
				<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z"
					/>
				</svg>
			</button>
			<button
				type="button"
				class="p-2 rounded {viewMode === 'table' ? 'bg-white dark:bg-surface-600 shadow-sm' : ''}"
				onclick={() => (viewMode = 'table')}
				title="Table view"
			>
				<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M4 6h16M4 10h16M4 14h16M4 18h16"
					/>
				</svg>
			</button>
		</div>
	</div>

	<!-- Error State -->
	{#if $workersStore.error}
		<div class="rq-card p-4">
			<p class="text-error-500">Failed to load workers: {$workersStore.error}</p>
		</div>
	{/if}

	<!-- Workers List -->
	{#if $workersStore.loading && filteredWorkers.length === 0}
		{#if viewMode === 'cards'}
			<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
				{#each [1, 2, 3] as i (i)}
					<div class="rq-card p-5">
						<div class="animate-pulse space-y-3">
							<div class="h-5 bg-surface-300 dark:bg-surface-600 rounded w-2/3"></div>
							<div class="h-4 bg-surface-300 dark:bg-surface-600 rounded w-1/2"></div>
							<div class="h-12 bg-surface-300 dark:bg-surface-600 rounded"></div>
						</div>
					</div>
				{/each}
			</div>
		{:else}
			<div class="table-container">
				<table class="table">
					<tbody>
						{#each [1, 2, 3] as i (i)}
							<tr>
								<td colspan="9">
									<div class="animate-pulse h-4 bg-surface-300 dark:bg-surface-600 rounded"></div>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	{:else if filteredWorkers.length === 0}
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
			<p class="text-sm text-surface-400 mt-1">
				{statusFilter ? 'No workers match the selected filter' : 'Workers will appear here when they connect'}
			</p>
		</div>
	{:else if viewMode === 'cards'}
		<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
			{#each filteredWorkers as worker (worker.worker_id)}
				<WorkerCard {worker} onclick={() => navigateToWorker(worker.worker_id)} />
			{/each}
		</div>
	{:else}
		<div class="table-container">
			<table class="rq-table">
				<thead>
					<tr>
						<th>Worker ID</th>
						<th>Status</th>
						<th>Concurrency</th>
						<th>CPU</th>
						<th>RAM</th>
						<th>GPU</th>
						<th>Queues</th>
						<th>Started</th>
						<th>Last Beat</th>
					</tr>
				</thead>
				<tbody>
					{#each filteredWorkers as worker (worker.worker_id)}
						{@const wm = worker.metrics}
						{@const memPct = wm && wm.memory_total_bytes > 0 ? (wm.memory_used_bytes / wm.memory_total_bytes) * 100 : 0}
						<tr class="cursor-pointer" onclick={() => navigateToWorker(worker.worker_id)}>
							<td class="font-mono text-sm">{worker.worker_id}</td>
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
							<td class="font-mono">{worker.concurrency}</td>
							<td>
								{#if wm}
									<div class="flex items-center gap-2 min-w-[80px]">
										<div class="rq-progress-track w-16">
											<div class="h-full rounded-full {barColor(wm.cpu_percent)}" style="width: {Math.min(wm.cpu_percent, 100)}%"></div>
										</div>
										<span class="text-xs text-surface-500">{wm.cpu_percent.toFixed(0)}%</span>
									</div>
								{:else}
									<span class="text-xs text-surface-400">-</span>
								{/if}
							</td>
							<td>
								{#if wm}
									<div class="flex items-center gap-2 min-w-[100px]">
										<div class="rq-progress-track w-16">
											<div class="h-full rounded-full {barColor(memPct)}" style="width: {Math.min(memPct, 100)}%"></div>
										</div>
										<span class="text-xs text-surface-500">{formatBytes(wm.memory_used_bytes)}</span>
									</div>
								{:else}
									<span class="text-xs text-surface-400">-</span>
								{/if}
							</td>
							<td>
								{#if wm?.gpus && wm.gpus.length > 0}
									<div class="space-y-1">
										{#each wm.gpus as gpu (gpu.index)}
											<div class="flex items-center gap-2 min-w-[100px]">
												<div class="rq-progress-track w-16">
													<div class="h-full rounded-full {barColor(gpu.utilization_percent)}" style="width: {Math.min(gpu.utilization_percent, 100)}%"></div>
												</div>
												<span class="text-xs text-surface-500" title={gpu.name}>{gpu.utilization_percent.toFixed(0)}% {gpu.temperature_c}&deg;C</span>
											</div>
										{/each}
									</div>
								{:else}
									<span class="text-xs text-surface-400">-</span>
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
											{:else}
												<span class="text-surface-400 ml-1">p:{queue.priority}</span>
											{/if}
										</span>
									{/each}
								</div>
							</td>
							<td class="text-surface-500">{formatRelativeTime(new Date(worker.started_at * 1000))}</td>
							<td class="text-surface-500">{formatRelativeTime(new Date(worker.last_beat * 1000))}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>
