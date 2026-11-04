<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { base } from '$app/paths';
	import { workersStore, workerStats } from '$lib/stores/workers';
	import { settings } from '$lib/stores/settings';
	import WorkerCard from '$lib/components/WorkerCard.svelte';
	import { formatRelativeTime } from '$lib/utils/format';

	let pollInterval: ReturnType<typeof setInterval> | null = null;
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
		if (worker.status === 'draining' || worker.status === 'drained') return worker.status;
		if (worker.status === 'stopped') return 'stopped';
		if (worker.status === 'running' && isWorkerProcessing(worker.worker_id)) return 'processing';
		if (worker.status === 'running') return 'idle';
		return 'stopped'; // fallback for unknown statuses
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
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
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
	</div>

	<!-- Error State -->
	{#if $workersStore.error}
		<div class="rq-card p-4">
			<p class="text-error-500">Failed to load workers: {$workersStore.error}</p>
		</div>
	{/if}

	<!-- Workers List -->
	{#if $workersStore.loading && filteredWorkers.length === 0}
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
	{:else}
		<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
			{#each filteredWorkers as worker (worker.worker_id)}
				<WorkerCard {worker} onclick={() => navigateToWorker(worker.worker_id)} />
			{/each}
		</div>
	{/if}
</div>
