<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { base } from '$app/paths';
	import { queuesStore } from '$lib/stores/queues';
	import { workersStore } from '$lib/stores/workers';
	import { settings } from '$lib/stores/settings';
	import { toaster } from '$lib/stores/toaster';
	import { pauseQueue, resumeQueue, deleteQueue, createQueueConfig, getQueueConfig, getQueueConfigs, updateQueueConfig } from '$lib/api/client';
	import { formatNumber, formatBytes, formatDuration, truncateId } from '$lib/utils/format';
	import { groupQueues, hasMultiQueueGroups, parseQueueName } from '$lib/utils/queueGrouping';
	import QueueCard from '$lib/components/QueueCard.svelte';
	import QueueGroupCard from '$lib/components/QueueGroupCard.svelte';
	import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
	import QueueConfigModal from '$lib/components/QueueConfigModal.svelte';
	import type { Worker, QueueConfigDetail, DeploymentConfig } from '$lib/api/types';

	let queueConfigMap = $state<Map<string, QueueConfigDetail>>(new Map());
	let pollInterval: ReturnType<typeof setInterval> | null = null;
	let searchQuery = $state('');
	let showPaused: boolean | null = $state(null);
	let refreshing = $state(false);
	// Cards read far better than the 11-column table on small screens.
	let viewMode = $state<'cards' | 'table'>(
		typeof window !== 'undefined' && window.innerWidth < 768 ? 'cards' : 'table'
	);
	let actionLoading = $state<string | null>(null);
	let groupQueuesEnabled = $state(true);
	let expandedGroups = $state<Set<string>>(new Set());
	let confirmDialog = $state({
		open: false,
		title: '',
		message: '',
		queueName: '',
		action: () => {}
	});

	// Queue config modal state
	let queueConfigModalOpen = $state(false);
	let queueConfigModalMode = $state<'create' | 'edit'>('create');
	let queueConfigModalLoading = $state(false);
	let selectedQueueConfig = $state<QueueConfigDetail | null>(null);

	let filteredQueues = $derived(
		$queuesStore.queues.filter((q) => {
			if (searchQuery && !q.queue.toLowerCase().includes(searchQuery.toLowerCase())) {
				return false;
			}
			if (showPaused !== null && q.paused !== showPaused) {
				return false;
			}
			return true;
		})
	);

	// Grouped queues
	let queueGroups = $derived(groupQueues(filteredQueues));
	let showGroupToggle = $derived(hasMultiQueueGroups($queuesStore.queues));

	function toggleGroupExpanded(groupName: string) {
		const newSet = new Set(expandedGroups);
		if (newSet.has(groupName)) {
			newSet.delete(groupName);
		} else {
			newSet.add(groupName);
		}
		expandedGroups = newSet;
	}

	// Parse worker queues string like "map[queue1:5 queue2:5]" to get queue names
	function parseWorkerQueues(queuesStr: string): string[] {
		const match = queuesStr.match(/map\[(.*)]/);
		if (!match) return [];
		return match[1].split(' ').map(q => q.split(':')[0]).filter(Boolean);
	}

	// Get workers for a specific queue
	function getWorkersForQueue(queueName: string): Worker[] {
		return $workersStore.workers.filter(w => {
			const workerQueues = parseWorkerQueues(w.queues);
			return workerQueues.includes(queueName);
		});
	}

	async function loadData() {
		await Promise.all([
			queuesStore.fetch(),
			workersStore.fetch(),
			loadQueueConfigs()
		]);
	}

	async function loadQueueConfigs() {
		try {
			const response = await getQueueConfigs();
			const map = new Map<string, QueueConfigDetail>();
			for (const q of response.queues) {
				map.set(q.name, q);
			}
			queueConfigMap = map;
		} catch (e) {
			console.error('Failed to load queue configs:', e);
		}
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

	async function handlePause(qname: string) {
		actionLoading = `pause-${qname}`;
		try {
			await pauseQueue(qname);
			await loadData();
			toaster.success({ title: 'Queue Paused', description: `Queue "${qname}" paused` });
		} catch (e) {
			console.error('Failed to pause queue:', e);
			toaster.error({ title: 'Error', description: `Failed to pause queue "${qname}"` });
		} finally {
			actionLoading = null;
		}
	}

	async function handleResume(qname: string) {
		actionLoading = `resume-${qname}`;
		try {
			await resumeQueue(qname);
			await loadData();
			toaster.success({ title: 'Queue Resumed', description: `Queue "${qname}" resumed` });
		} catch (e) {
			console.error('Failed to resume queue:', e);
			toaster.error({ title: 'Error', description: `Failed to resume queue "${qname}"` });
		} finally {
			actionLoading = null;
		}
	}

	function confirmDelete(qname: string) {
		confirmDialog = {
			open: true,
			title: 'Delete Queue',
			message: `Are you sure you want to delete queue "${qname}"? This will permanently remove all tasks in this queue. This action cannot be undone.`,
			queueName: qname,
			action: async () => {
				actionLoading = `delete-${qname}`;
				try {
					await deleteQueue(qname);
					await loadData();
					toaster.success({ title: 'Queue Deleted', description: `Queue "${qname}" deleted` });
				} catch (e) {
					console.error('Failed to delete queue:', e);
					toaster.error({ title: 'Error', description: `Failed to delete queue "${qname}"` });
				} finally {
					actionLoading = null;
				}
			}
		};
	}

	function handleSearchInput(e: Event) {
		searchQuery = (e.target as HTMLInputElement).value;
	}

	function openCreateQueueConfig() {
		selectedQueueConfig = null;
		queueConfigModalMode = 'create';
		queueConfigModalOpen = true;
	}

	async function openEditQueueConfig(qname: string) {
		try {
			const config = await getQueueConfig(qname);
			selectedQueueConfig = config;
			queueConfigModalMode = 'edit';
			queueConfigModalOpen = true;
		} catch (e) {
			toaster.error({ title: 'Error', description: `Failed to load queue config: ${e instanceof Error ? e.message : 'Unknown error'}` });
		}
	}

	interface QueueToCreate {
		name: string;
		priority: number;
		deployment: DeploymentConfig | null;
	}

	async function handleSaveQueueConfig(queues: QueueToCreate[]) {
		queueConfigModalLoading = true;
		try {
			if (queueConfigModalMode === 'create') {
				// Create queues sequentially to avoid SQLite locking issues
				let succeeded = 0;
				let failed = 0;
				const errors: string[] = [];

				for (const q of queues) {
					try {
						await createQueueConfig(q.name, q.priority, q.deployment || undefined);
						succeeded++;
					} catch (e) {
						failed++;
						errors.push(e instanceof Error ? e.message : 'Unknown error');
					}
				}

				if (failed > 0) {
					toaster.error({ title: 'Partial Failure', description: `Created ${succeeded} queue(s), ${failed} failed: ${errors[0]}` });
				} else if (succeeded === 1) {
					toaster.success({ title: 'Queue Created', description: `Queue "${queues[0].name}" created` });
				} else {
					toaster.success({ title: 'Queues Created', description: `${succeeded} queues created` });
				}
			} else {
				// Edit mode - single queue
				const q = queues[0];
				await updateQueueConfig(q.name, q.priority, q.deployment || undefined);
				toaster.success({ title: 'Queue Updated', description: `Queue "${q.name}" updated` });
			}
			await loadData();
			queueConfigModalOpen = false;
		} catch (e) {
			const errorMessage = e instanceof Error ? e.message : 'Failed to save queue config';
			toaster.error({ title: 'Error', description: errorMessage });
		} finally {
			queueConfigModalLoading = false;
		}
	}

	async function handleSaveSubqueue(parentQueue: string, subqueueName: string, priority: number) {
		queueConfigModalLoading = true;
		const fullName = `${parentQueue}.${subqueueName}`;
		try {
			await createQueueConfig(fullName, priority);
			toaster.success({ title: 'Subqueue Created', description: `Subqueue "${fullName}" created` });
			await loadData();
			queueConfigModalOpen = false;
		} catch (e) {
			const errorMessage = e instanceof Error ? e.message : 'Failed to create subqueue';
			toaster.error({ title: 'Error', description: errorMessage });
		} finally {
			queueConfigModalLoading = false;
		}
	}
</script>

<svelte:head>
	<title>Queues - runqy Monitor</title>
</svelte:head>

<div class="rq-page space-y-8">
	<!-- Header -->
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<h1 class="rq-page-title">Queues</h1>
			<p class="text-surface-500">{filteredQueues.length} queue{filteredQueues.length !== 1 ? 's' : ''}</p>
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
			<button type="button" class="rq-btn-primary" onclick={openCreateQueueConfig}>
				<svg class="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
				</svg>
				Create Queue
			</button>
		</div>
	</div>

	<!-- Filters -->
	<div class="flex flex-wrap items-center gap-4">
		<div class="relative flex-1 min-w-[200px] max-w-md">
			<svg
				class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-surface-500"
				fill="none"
				stroke="currentColor"
				viewBox="0 0 24 24"
			>
				<path
					stroke-linecap="round"
					stroke-linejoin="round"
					stroke-width="2"
					d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
				/>
			</svg>
			<input
				type="text"
				value={searchQuery}
				oninput={handleSearchInput}
				placeholder="Search queues..."
				class="input pl-10 pr-4 py-2 w-full"
			/>
		</div>

		<div class="flex items-center gap-2">
			<button
				type="button"
				class="{showPaused === null ? 'rq-pill-active' : 'rq-pill'}"
				onclick={() => (showPaused = null)}
			>
				All
			</button>
			<button
				type="button"
				class="{showPaused === false ? 'rq-pill-active' : 'rq-pill'}"
				onclick={() => (showPaused = false)}
			>
				Running
			</button>
			<button
				type="button"
				class="{showPaused === true ? 'rq-pill-active' : 'rq-pill'}"
				onclick={() => (showPaused = true)}
			>
				Paused
			</button>
		</div>

		<!-- Group Toggle -->
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

		<!-- View Toggle -->
		<div class="flex items-center gap-1 bg-surface-200 dark:bg-surface-700 rounded-lg p-1 ml-auto">
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
	{#if $queuesStore.error}
		<div class="rq-card p-4">
			<p class="text-error-500">Failed to load queues: {$queuesStore.error}</p>
		</div>
	{/if}

	<!-- Loading State -->
	{#if $queuesStore.loading && filteredQueues.length === 0}
		{#if viewMode === 'cards'}
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
		{:else}
			<div class="table-container">
				<table class="rq-table">
					<tbody>
						{#each [1, 2, 3, 4, 5] as i (i)}
							<tr>
								<td colspan="10">
									<div class="animate-pulse h-4 bg-surface-300 dark:bg-surface-600 rounded"></div>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	{:else if filteredQueues.length === 0}
		<div class="card preset-outlined-surface-200-800 bg-surface-50-950 p-8 text-center">
			<svg class="w-12 h-12 mx-auto text-surface-400 mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path
					stroke-linecap="round"
					stroke-linejoin="round"
					stroke-width="2"
					d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10"
				/>
			</svg>
			<p class="text-surface-500">{searchQuery ? 'No queues match your search' : 'No queues found'}</p>
		</div>
	{:else if viewMode === 'cards'}
		<!-- Card View -->
		{#if groupQueuesEnabled}
			<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
				{#each queueGroups as group (group.name)}
					<QueueGroupCard {group} workers={$workersStore.workers} onQueueClick={navigateToQueue} />
				{/each}
			</div>
		{:else}
			<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
				{#each filteredQueues as queue (queue.queue)}
					<QueueCard
						{queue}
						workers={$workersStore.workers}
						redisStorage={queueConfigMap.get(queue.queue)?.deployment?.redis_storage ?? false}
						onClick={() => navigateToQueue(queue.queue)}
						onPause={() => handlePause(queue.queue)}
						onResume={() => handleResume(queue.queue)}
						onDelete={() => confirmDelete(queue.queue)}
					/>
				{/each}
			</div>
		{/if}
	{:else}
		<!-- Table View -->
		<div class="table-container">
			<table class="rq-table">
				<thead>
					<tr>
						<th>Name</th>
						<th>Status</th>
						<th>Storage</th>
						<th>Workers</th>
						<th class="text-right">Pending</th>
						<th class="text-right">Active</th>
						<th class="text-right">Retry</th>
						<th class="text-right">Completed</th>
						<th class="text-right">Failed</th>
						<th class="text-right">Latency</th>
						<th>Actions</th>
					</tr>
				</thead>
				<tbody>
					{#if groupQueuesEnabled}
						{#each queueGroups as group (group.name)}
							{@const isExpanded = expandedGroups.has(group.name)}
							{@const hasSubQueues = group.queues.length > 1}
							{@const groupRedisStorage = queueConfigMap.get(group.queues[0]?.queue)?.deployment?.redis_storage}
							<!-- Parent/Group Row -->
							<tr
								class="cursor-pointer {hasSubQueues ? 'bg-surface-50 dark:bg-surface-800' : ''}"
								onclick={() => hasSubQueues ? toggleGroupExpanded(group.name) : navigateToQueue(group.queues[0].queue)}
							>
								<td>
									<div class="flex items-center gap-2">
										{#if hasSubQueues}
											<svg
												class="w-4 h-4 text-surface-500 transition-transform {isExpanded ? 'rotate-90' : ''}"
												fill="none"
												stroke="currentColor"
												viewBox="0 0 24 24"
											>
												<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
											</svg>
										{/if}
										<span class="font-semibold text-primary-500 hover:text-primary-600">{group.name}</span>
										{#if hasSubQueues}
											<span class="badge preset-outlined-surface-500 text-xs">{group.queues.length} queues</span>
										{/if}
									</div>
								</td>
								<td>
									{#if group.paused}
										<span class="badge preset-filled-warning-500 text-xs">Paused</span>
									{:else}
										<span class="badge preset-filled-success-500 text-xs">Running</span>
									{/if}
								</td>
								<td>
									{#if groupRedisStorage}
										<span class="badge preset-filled-success-500 text-xs">Redis</span>
									{:else}
										<span class="badge preset-filled-surface-500 text-xs">None</span>
									{/if}
								</td>
								<td>
									{#if !hasSubQueues}
										<!-- Single .default queue: the group row IS the queue row, show its workers -->
										{@const queueWorkers = getWorkersForQueue(group.queues[0].queue)}
										{#if queueWorkers.length === 0}
											<span class="warning-badge text-xs px-2 py-1 rounded inline-flex items-center gap-1">
												<svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
													<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
												</svg>
												None
											</span>
										{:else}
											<div class="flex items-center gap-1 flex-wrap">
												{#each queueWorkers.slice(0, 2) as worker (worker.worker_id)}
													<span class="badge preset-outlined-primary-500 text-xs" title={worker.worker_id}>
														{truncateId(worker.worker_id, 10)}
													</span>
												{/each}
												{#if queueWorkers.length > 2}
													<span class="text-xs text-surface-500">+{queueWorkers.length - 2}</span>
												{/if}
											</div>
										{/if}
									{:else}
										<span class="text-xs text-surface-500">—</span>
									{/if}
								</td>
								<td class="text-right font-mono text-warning-500 font-semibold">{formatNumber(group.pending)}</td>
								<td class="text-right font-mono text-success-500 font-semibold">{formatNumber(group.active)}</td>
								<td class="text-right font-mono text-warning-600 font-semibold">{formatNumber(group.retry)}</td>
								<td class="text-right font-mono text-tertiary-500 font-semibold">{formatNumber(group.completed)}</td>
								<td class="text-right font-mono text-error-500 font-semibold">{formatNumber(group.failed)}</td>
								<td class="text-right text-surface-500">{formatDuration(group.latency_msec)}</td>
								<td>
									{#if !hasSubQueues}
										<div class="flex items-center gap-1" onclick={(e) => e.stopPropagation()}>
											{#if group.queues[0].paused}
												<button
													type="button"
													class="btn btn-sm preset-outlined-success-500"
													onclick={() => handleResume(group.queues[0].queue)}
													title="Resume"
												>
													<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
														<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
														<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
													</svg>
												</button>
											{:else}
												<button
													type="button"
													class="btn btn-sm preset-outlined-warning-500"
													onclick={() => handlePause(group.queues[0].queue)}
													title="Pause"
												>
													<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
														<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z" />
													</svg>
												</button>
											{/if}
											<button
												type="button"
												class="btn btn-sm preset-outlined-error-500"
												onclick={() => confirmDelete(group.queues[0].queue)}
												title="Delete"
											>
												<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
													<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
												</svg>
											</button>
										</div>
									{:else}
										<span class="text-xs text-surface-400">—</span>
									{/if}
								</td>
							</tr>
							<!-- Sub-queue Rows (when expanded) -->
							{#if hasSubQueues && isExpanded}
								{#each group.queues as queue (queue.queue)}
									{@const queueWorkers = getWorkersForQueue(queue.queue)}
									{@const subqueueName = parseQueueName(queue.queue).subqueue || queue.queue}
									{@const subRedisStorage = queueConfigMap.get(queue.queue)?.deployment?.redis_storage}
									<tr class="cursor-pointer hover:bg-surface-100 dark:hover:bg-surface-700" onclick={() => navigateToQueue(queue.queue)}>
										<td>
											<div class="pl-8 flex items-center gap-2">
												<span class="text-surface-400">└</span>
												<span class="font-medium text-primary-500 hover:text-primary-600">{subqueueName}</span>
											</div>
										</td>
										<td>
											{#if queue.paused}
												<span class="badge preset-filled-warning-500 text-xs">Paused</span>
											{:else}
												<span class="badge preset-filled-success-500 text-xs">Running</span>
											{/if}
										</td>
										<td>
											{#if subRedisStorage}
												<span class="badge preset-filled-success-500 text-xs">Redis</span>
											{:else}
												<span class="badge preset-filled-surface-500 text-xs">None</span>
											{/if}
										</td>
										<td>
											{#if queueWorkers.length === 0}
												<span class="warning-badge text-xs px-2 py-1 rounded inline-flex items-center gap-1">
													<svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
														<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
													</svg>
													None
												</span>
											{:else}
												<div class="flex items-center gap-1 flex-wrap">
													{#each queueWorkers.slice(0, 2) as worker (worker.worker_id)}
														<span class="badge preset-outlined-primary-500 text-xs" title={worker.worker_id}>
															{truncateId(worker.worker_id, 10)}
														</span>
													{/each}
													{#if queueWorkers.length > 2}
														<span class="text-xs text-surface-500">+{queueWorkers.length - 2}</span>
													{/if}
												</div>
											{/if}
										</td>
										<td class="text-right font-mono text-warning-500">{formatNumber(queue.pending)}</td>
										<td class="text-right font-mono text-success-500">{formatNumber(queue.active)}</td>
										<td class="text-right font-mono text-warning-600">{formatNumber(queue.retry)}</td>
										<td class="text-right font-mono text-tertiary-500">{formatNumber(queue.completed)}</td>
										<td class="text-right font-mono text-error-500">{formatNumber(queue.failed ?? 0)}</td>
										<td class="text-right text-surface-500">{formatDuration(queue.latency_msec ?? 0)}</td>
										<td>
											<div class="flex items-center gap-1" onclick={(e) => e.stopPropagation()}>
												{#if queue.paused}
													<button
														type="button"
														class="btn btn-sm preset-outlined-success-500"
														onclick={() => handleResume(queue.queue)}
														title="Resume"
													>
														<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
															<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
															<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
														</svg>
													</button>
												{:else}
													<button
														type="button"
														class="btn btn-sm preset-outlined-warning-500"
														onclick={() => handlePause(queue.queue)}
														title="Pause"
													>
														<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
															<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z" />
														</svg>
													</button>
												{/if}
												<button
													type="button"
													class="btn btn-sm preset-outlined-error-500"
													onclick={() => confirmDelete(queue.queue)}
													title="Delete"
												>
													<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
														<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
													</svg>
												</button>
											</div>
										</td>
									</tr>
								{/each}
							{/if}
						{/each}
					{:else}
						{#each filteredQueues as queue (queue.queue)}
							{@const queueWorkers = getWorkersForQueue(queue.queue)}
							{@const queueRedisStorage = queueConfigMap.get(queue.queue)?.deployment?.redis_storage}
							<tr class="cursor-pointer" onclick={() => navigateToQueue(queue.queue)}>
								<td>
									<span class="font-semibold text-primary-500 hover:text-primary-600">{queue.queue}</span>
								</td>
								<td>
									{#if queue.paused}
										<span class="badge preset-filled-warning-500 text-xs">Paused</span>
									{:else}
										<span class="badge preset-filled-success-500 text-xs">Running</span>
									{/if}
								</td>
								<td>
									{#if queueRedisStorage}
										<span class="badge preset-filled-success-500 text-xs">Redis</span>
									{:else}
										<span class="badge preset-filled-surface-500 text-xs">None</span>
									{/if}
								</td>
								<td>
									{#if queueWorkers.length === 0}
										<span class="warning-badge text-xs px-2 py-1 rounded inline-flex items-center gap-1">
											<svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
												<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
											</svg>
											None
										</span>
									{:else}
										<div class="flex items-center gap-1 flex-wrap">
											{#each queueWorkers.slice(0, 2) as worker (worker.worker_id)}
												<span class="badge preset-outlined-primary-500 text-xs" title={worker.worker_id}>
													{truncateId(worker.worker_id, 10)}
												</span>
											{/each}
											{#if queueWorkers.length > 2}
												<span class="text-xs text-surface-500">+{queueWorkers.length - 2}</span>
											{/if}
										</div>
									{/if}
								</td>
								<td class="text-right font-mono text-warning-500">{formatNumber(queue.pending)}</td>
								<td class="text-right font-mono text-success-500">{formatNumber(queue.active)}</td>
								<td class="text-right font-mono text-warning-600">{formatNumber(queue.retry)}</td>
								<td class="text-right font-mono text-tertiary-500">{formatNumber(queue.completed)}</td>
								<td class="text-right font-mono text-error-500">{formatNumber(queue.failed ?? 0)}</td>
								<td class="text-right text-surface-500">{formatDuration(queue.latency_msec ?? 0)}</td>
								<td>
									<div class="flex items-center gap-1" onclick={(e) => e.stopPropagation()}>
										{#if queue.paused}
											<button
												type="button"
												class="btn btn-sm preset-outlined-success-500 {actionLoading === `resume-${queue.queue}` ? 'opacity-50' : ''}"
												onclick={() => handleResume(queue.queue)}
												disabled={actionLoading === `resume-${queue.queue}`}
												title="Resume"
											>
												{#if actionLoading === `resume-${queue.queue}`}
													<svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
														<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
														<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
													</svg>
												{:else}
													<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
														<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
														<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
													</svg>
												{/if}
											</button>
										{:else}
											<button
												type="button"
												class="btn btn-sm preset-outlined-warning-500 {actionLoading === `pause-${queue.queue}` ? 'opacity-50' : ''}"
												onclick={() => handlePause(queue.queue)}
												disabled={actionLoading === `pause-${queue.queue}`}
												title="Pause"
											>
												{#if actionLoading === `pause-${queue.queue}`}
													<svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
														<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
														<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
													</svg>
												{:else}
													<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
														<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z" />
													</svg>
												{/if}
											</button>
										{/if}
										<button
											type="button"
											class="btn btn-sm preset-outlined-primary-500"
											onclick={() => openEditQueueConfig(queue.queue)}
											title="Edit Config"
										>
											<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
												<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"/>
												<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"/>
											</svg>
										</button>
										<button
											type="button"
											class="btn btn-sm preset-outlined-error-500 {actionLoading === `delete-${queue.queue}` ? 'opacity-50' : ''}"
											onclick={() => confirmDelete(queue.queue)}
											disabled={actionLoading === `delete-${queue.queue}`}
											title="Delete"
										>
											{#if actionLoading === `delete-${queue.queue}`}
												<svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
													<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
													<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
												</svg>
											{:else}
												<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
													<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
												</svg>
											{/if}
										</button>
									</div>
								</td>
							</tr>
						{/each}
					{/if}
				</tbody>
			</table>
		</div>
	{/if}
</div>

<ConfirmDialog
	bind:open={confirmDialog.open}
	title={confirmDialog.title}
	message={confirmDialog.message}
	variant="danger"
	confirmText="Delete"
	onconfirm={confirmDialog.action}
/>

<QueueConfigModal
	bind:open={queueConfigModalOpen}
	loading={queueConfigModalLoading}
	mode={queueConfigModalMode}
	config={selectedQueueConfig}
	existingQueues={$queuesStore.queues.map(q => q.queue)}
	onsave={handleSaveQueueConfig}
	onsavesubqueue={handleSaveSubqueue}
/>
