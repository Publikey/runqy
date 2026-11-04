<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { base } from '$app/paths';
	import { Cpu } from '@lucide/svelte';
	import { workersStore, workerStats } from '$lib/stores/workers';
	import { settings } from '$lib/stores/settings';
	import WorkerCard from '$lib/components/WorkerCard.svelte';
	import { Callout, EmptyState, PageHeader, Segmented, Shimmer, StatTile } from '$lib/components/ui';
	import { workerState, type WorkerState } from '$lib/utils/status';

	type Filter = 'all' | 'processing' | 'idle' | 'bootstrapping' | 'stale' | 'stopped';

	let pollInterval: ReturnType<typeof setInterval> | null = null;
	let statusFilter = $state<Filter>('all');

	function navigateToWorker(workerId: string) {
		goto(`${base}/workers/${encodeURIComponent(workerId)}`);
	}

	let states = $derived(
		$workersStore.workers.map((w) => ({ worker: w, state: workerState(w, $workersStore.servers) }))
	);

	function countState(s: WorkerState): number {
		return states.filter((x) => x.state === s).length;
	}

	let filterItems = $derived<{ value: Filter; label: string; count: number }[]>([
		{ value: 'all', label: 'All', count: states.length },
		{ value: 'processing', label: 'Processing', count: countState('processing') },
		{ value: 'idle', label: 'Idle', count: countState('idle') },
		{ value: 'bootstrapping', label: 'Bootstrapping', count: countState('bootstrapping') },
		{ value: 'stale', label: 'Stale', count: countState('stale') },
		{ value: 'stopped', label: 'Stopped', count: countState('stopped') }
	]);

	let filteredWorkers = $derived(
		states.filter((x) => statusFilter === 'all' || x.state === statusFilter).map((x) => x.worker)
	);

	async function loadData() {
		await workersStore.fetch();
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

<div class="rq-page space-y-4 md:space-y-6">
	<PageHeader
		title="Workers"
		subtitle="{filteredWorkers.length} worker{filteredWorkers.length !== 1 ? 's' : ''}"
		updatedAt={$workersStore.lastUpdated}
		onrefresh={loadData}
	/>

	<!-- Phones: the filter below already shows these counts. -->
	<div class="hidden md:grid grid-cols-3 xl:grid-cols-6 gap-4">
		<StatTile label="Total workers" value={$workerStats.total} />
		<StatTile label="Processing" value={$workerStats.processing} tone={$workerStats.processing > 0 ? 'accent' : 'default'} />
		<StatTile label="Idle" value={$workerStats.idle} />
		<StatTile label="Bootstrapping" value={$workerStats.bootstrapping} />
		<StatTile label="Stale" value={$workerStats.stale} tone={$workerStats.stale > 0 ? 'warning' : 'default'} />
		<StatTile label="Stopped" value={$workerStats.stopped} tone="neutral" />
	</div>

	<div class="max-w-full min-w-0">
		<Segmented items={filterItems} bind:value={statusFilter} label="Filter by status" />
	</div>

	{#if $workersStore.error}
		<Callout>Failed to load workers: {$workersStore.error}</Callout>
	{/if}

	{#if $workersStore.loading && filteredWorkers.length === 0}
		<div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
			{#each [1, 2, 3] as i (i)}
				<div class="rq-card p-4 space-y-3">
					<Shimmer class="h-4 w-2/3" />
					<Shimmer class="h-3 w-1/3" />
					<Shimmer class="h-12 w-full" />
				</div>
			{/each}
		</div>
	{:else if filteredWorkers.length === 0}
		<div class="rq-card">
			<EmptyState
				icon={Cpu}
				title="No workers found"
				description={statusFilter !== 'all'
					? 'No workers match the selected filter'
					: 'Workers will appear here when they connect'}
			/>
		</div>
	{:else}
		<div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
			{#each filteredWorkers as worker (worker.worker_id)}
				<WorkerCard {worker} onclick={() => navigateToWorker(worker.worker_id)} />
			{/each}
		</div>
	{/if}
</div>
