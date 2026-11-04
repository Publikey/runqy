<script lang="ts">
	import { workerQueueNames } from '$lib/utils/workerQueues';
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { base } from '$app/paths';
	import { queuesStore } from '$lib/stores/queues';
	import { workersStore } from '$lib/stores/workers';
	import { settings } from '$lib/stores/settings';
	import { toaster } from '$lib/stores/toaster';
	import { pauseQueue, resumeQueue, deleteQueue, createQueueConfig, getQueueConfig, getQueueConfigs, updateQueueConfig } from '$lib/api/client';
	import { formatNumber, formatDuration } from '$lib/utils/format';
	import { groupQueues, parseQueueName, type QueueGroup } from '$lib/utils/queueGrouping';
	import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
	import QueueConfigModal from '$lib/components/QueueConfigModal.svelte';
	import type { Queue, Worker, QueueConfigDetail, DeploymentConfig } from '$lib/api/types';

	let queueConfigMap = $state<Map<string, QueueConfigDetail>>(new Map());
	let pollInterval: ReturnType<typeof setInterval> | null = null;
	let searchQuery = $state('');
	let showPaused: boolean | null = $state(null);
	let refreshing = $state(false);
	let actionLoading = $state<string | null>(null);
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

	// A search on the parent name keeps all its sub-queues, so the group total stays whole.
	let filteredQueues = $derived(
		$queuesStore.queues.filter((q) => {
			if (searchQuery) {
				const s = searchQuery.toLowerCase();
				if (!q.queue.toLowerCase().includes(s) && !parseQueueName(q.queue).parent.toLowerCase().includes(s)) {
					return false;
				}
			}
			if (showPaused !== null && q.paused !== showPaused) {
				return false;
			}
			return true;
		})
	);

	function priorityOf(queueName: string): number {
		return queueConfigMap.get(queueName)?.priority ?? 0;
	}

	// One block per parent queue: its sub-queues by descending priority, then a total.
	let queueGroups = $derived(
		groupQueues(filteredQueues).map((g) => ({
			...g,
			queues: [...g.queues].sort(
				(a, b) => priorityOf(b.queue) - priorityOf(a.queue) || a.queue.localeCompare(b.queue)
			)
		}))
	);

	let subQueueCount = $derived(filteredQueues.length);

	// Get workers for a specific queue
	function getWorkersForQueue(queueName: string): Worker[] {
		return $workersStore.workers.filter(w => {
			const workerQueues = workerQueueNames(w.queues);
			return workerQueues.includes(queueName);
		});
	}

	// Distinct workers serving any sub-queue of a group.
	function getWorkersForGroup(group: QueueGroup): Worker[] {
		const names = new Set(group.queues.map((q) => q.queue));
		return $workersStore.workers.filter((w) => workerQueueNames(w.queues).some((n) => names.has(n)));
	}

	function subName(q: Queue): string {
		return parseQueueName(q.queue).subqueue || q.queue;
	}

	function workersTitle(ws: Worker[]): string {
		return ws.length ? ws.map((w) => w.worker_id).join('\n') : 'No worker listens on this queue';
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
		// The dashboard links here with ?q=<parent> to show one queue's block.
		const q = new URLSearchParams(window.location.search).get('q');
		if (q) searchQuery = q;
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
			<p class="text-surface-500">
				{queueGroups.length} queue{queueGroups.length !== 1 ? 's' : ''} · {subQueueCount} sub-queue{subQueueCount !== 1 ? 's' : ''}
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
	</div>

	<!-- Error State -->
	{#if $queuesStore.error}
		<div class="rq-card p-4">
			<p class="text-error-500">Failed to load queues: {$queuesStore.error}</p>
		</div>
	{/if}

	<!-- Loading State -->
	{#if $queuesStore.loading && filteredQueues.length === 0}
		<div class="rq-card p-4 space-y-3">
			{#each [1, 2, 3, 4, 5] as i (i)}
				<div class="animate-pulse h-5 bg-surface-300 dark:bg-surface-600 rounded"></div>
			{/each}
		</div>
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
	{:else}
		<!-- Desktop: one table, a block per parent queue (header, sub-queues, total) -->
		<div class="hidden md:block table-container">
			<table class="rq-table">
				<thead>
					<tr>
						<th>Queue</th>
						<th>Status</th>
						<th>Workers</th>
						<th class="text-right">Pending</th>
						<th class="text-right">Active</th>
						<th class="text-right">Retry</th>
						<th class="text-right">Completed</th>
						<th class="text-right">Failed</th>
						<th class="text-right">Latency</th>
						<th class="text-right">Actions</th>
					</tr>
				</thead>
				{#each queueGroups as group (group.name)}
					{@const multi = group.queues.length > 1}
					<tbody>
						<tr class="bg-surface-100-900">
							<td colspan="9">
								{@render groupTitle(group)}
							</td>
							<td class="text-right">
								{@render editButton(group)}
							</td>
						</tr>
						{#each group.queues as queue (queue.queue)}
							<tr class="cursor-pointer" onclick={() => navigateToQueue(queue.queue)}>
								<td>
									<div class="pl-4 flex items-center gap-2">
										<span class="font-medium text-primary-500">{subName(queue)}</span>
										<span class="text-xs text-surface-400">p:{priorityOf(queue.queue)}</span>
									</div>
								</td>
								<td>{@render statusBadge(queue.paused)}</td>
								<td>{@render workersBadge(getWorkersForQueue(queue.queue))}</td>
								<td class="text-right font-mono text-warning-500">{formatNumber(queue.pending)}</td>
								<td class="text-right font-mono text-success-500">{formatNumber(queue.active)}</td>
								<td class="text-right font-mono text-warning-600">{formatNumber(queue.retry)}</td>
								<td class="text-right font-mono text-tertiary-500">{formatNumber(queue.completed)}</td>
								<td class="text-right font-mono text-error-500">{formatNumber(queue.failed ?? 0)}</td>
								<td class="text-right text-surface-500">{formatDuration(queue.latency_msec ?? 0)}</td>
								<td>
									<div class="flex justify-end">{@render queueActions(queue)}</div>
								</td>
							</tr>
						{/each}
						{#if multi}
							<tr class="font-semibold">
								<td><div class="pl-4 text-surface-400 uppercase text-xs tracking-wider">Total</div></td>
								<td>{@render groupStatus(group)}</td>
								<td>{@render workersBadge(getWorkersForGroup(group))}</td>
								<td class="text-right font-mono text-warning-500">{formatNumber(group.pending)}</td>
								<td class="text-right font-mono text-success-500">{formatNumber(group.active)}</td>
								<td class="text-right font-mono text-warning-600">{formatNumber(group.retry)}</td>
								<td class="text-right font-mono text-tertiary-500">{formatNumber(group.completed)}</td>
								<td class="text-right font-mono text-error-500">{formatNumber(group.failed)}</td>
								<td class="text-right text-surface-500" title="Highest sub-queue latency">{formatDuration(group.latency_msec)}</td>
								<td></td>
							</tr>
						{/if}
					</tbody>
				{/each}
			</table>
		</div>

		<!-- Mobile: the same list as stacked blocks, no horizontal scroll -->
		<div class="md:hidden space-y-4">
			{#each queueGroups as group (group.name)}
				{@const multi = group.queues.length > 1}
				<div class="rq-card overflow-hidden">
					<div class="flex items-center justify-between gap-2 px-4 py-3 bg-surface-100-900">
						{@render groupTitle(group)}
						{@render editButton(group)}
					</div>
					{#each group.queues as queue (queue.queue)}
						<!-- svelte-ignore a11y_click_events_have_key_events -->
						<!-- svelte-ignore a11y_no_static_element_interactions -->
						<div
							class="px-4 py-3 cursor-pointer"
							style="border-top: 1px solid var(--runqy-border-subtle);"
							onclick={() => navigateToQueue(queue.queue)}
						>
							<div class="flex items-center justify-between gap-2">
								<div class="flex items-center gap-2 min-w-0">
									<span class="font-medium text-primary-500 truncate">{subName(queue)}</span>
									<span class="text-xs text-surface-400">p:{priorityOf(queue.queue)}</span>
									{@render statusBadge(queue.paused)}
								</div>
								{@render queueActions(queue)}
							</div>
							<div class="mt-2">{@render workersBadge(getWorkersForQueue(queue.queue))}</div>
							{@render mobileStats(queue.pending, queue.active, queue.retry, queue.completed, queue.failed ?? 0, queue.latency_msec ?? 0)}
						</div>
					{/each}
					{#if multi}
						<div class="px-4 py-3 bg-surface-50-950" style="border-top: 1px solid var(--runqy-border-default);">
							<div class="flex items-center justify-between gap-2">
								<span class="text-xs font-semibold uppercase tracking-wider text-surface-400">Total</span>
								<div class="flex items-center gap-2">
									{@render groupStatus(group)}
									{@render workersBadge(getWorkersForGroup(group))}
								</div>
							</div>
							{@render mobileStats(group.pending, group.active, group.retry, group.completed, group.failed, group.latency_msec)}
						</div>
					{/if}
				</div>
			{/each}
		</div>
	{/if}
</div>

{#snippet groupTitle(group: QueueGroup)}
	{@const redis = queueConfigMap.get(group.queues[0]?.queue)?.deployment?.redis_storage}
	<div class="flex flex-wrap items-center gap-2 min-w-0">
		<span class="font-semibold font-mono truncate">{group.name}</span>
		{#if group.queues.length > 1}
			<span class="badge preset-outlined-surface-500 text-xs">{group.queues.length} sub-queues</span>
		{/if}
		{#if redis}
			<span class="badge preset-outlined-success-500 text-xs" title="Results stored in Redis">Redis storage</span>
		{/if}
	</div>
{/snippet}

{#snippet editButton(group: QueueGroup)}
	<button
		type="button"
		class="btn btn-sm preset-outlined-primary-500 shrink-0"
		onclick={(e) => { e.stopPropagation(); openEditQueueConfig(group.queues[0].queue); }}
		title="Edit queue config"
	>
		<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
			<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"/>
			<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"/>
		</svg>
	</button>
{/snippet}

{#snippet statusBadge(paused: boolean)}
	{#if paused}
		<span class="badge preset-filled-warning-500 text-xs">Paused</span>
	{:else}
		<span class="badge preset-filled-success-500 text-xs">Running</span>
	{/if}
{/snippet}

{#snippet groupStatus(group: QueueGroup)}
	{@const paused = group.queues.filter((q) => q.paused).length}
	{#if paused === 0}
		<span class="badge preset-filled-success-500 text-xs">Running</span>
	{:else if paused === group.queues.length}
		<span class="badge preset-filled-warning-500 text-xs">Paused</span>
	{:else}
		<span class="badge preset-outlined-warning-500 text-xs">{paused}/{group.queues.length} paused</span>
	{/if}
{/snippet}

{#snippet workersBadge(ws: Worker[])}
	{#if ws.length === 0}
		<span class="warning-badge text-xs px-2 py-1 rounded inline-flex items-center gap-1" title={workersTitle(ws)}>
			<svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
			</svg>
			No worker
		</span>
	{:else}
		<span class="badge preset-outlined-primary-500 text-xs" title={workersTitle(ws)}>
			{ws.length} worker{ws.length !== 1 ? 's' : ''}
		</span>
	{/if}
{/snippet}

{#snippet mobileStats(pending: number, active: number, retry: number, completed: number, failed: number, latency: number)}
	<div class="grid grid-cols-3 gap-x-3 gap-y-2 mt-3 text-center">
		<div>
			<div class="font-mono text-warning-500">{formatNumber(pending)}</div>
			<div class="text-[0.65rem] uppercase tracking-wider text-surface-400">Pending</div>
		</div>
		<div>
			<div class="font-mono text-success-500">{formatNumber(active)}</div>
			<div class="text-[0.65rem] uppercase tracking-wider text-surface-400">Active</div>
		</div>
		<div>
			<div class="font-mono text-warning-600">{formatNumber(retry)}</div>
			<div class="text-[0.65rem] uppercase tracking-wider text-surface-400">Retry</div>
		</div>
		<div>
			<div class="font-mono text-tertiary-500">{formatNumber(completed)}</div>
			<div class="text-[0.65rem] uppercase tracking-wider text-surface-400">Completed</div>
		</div>
		<div>
			<div class="font-mono text-error-500">{formatNumber(failed)}</div>
			<div class="text-[0.65rem] uppercase tracking-wider text-surface-400">Failed</div>
		</div>
		<div>
			<div class="text-surface-500">{formatDuration(latency)}</div>
			<div class="text-[0.65rem] uppercase tracking-wider text-surface-400">Latency</div>
		</div>
	</div>
{/snippet}

{#snippet spinner()}
	<svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
		<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
		<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
	</svg>
{/snippet}

{#snippet queueActions(queue: Queue)}
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div class="flex items-center gap-1 shrink-0" onclick={(e) => e.stopPropagation()}>
		{#if queue.paused}
			<button
				type="button"
				class="btn btn-sm preset-outlined-success-500"
				onclick={() => handleResume(queue.queue)}
				disabled={actionLoading === `resume-${queue.queue}`}
				title="Resume"
			>
				{#if actionLoading === `resume-${queue.queue}`}
					{@render spinner()}
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
				class="btn btn-sm preset-outlined-warning-500"
				onclick={() => handlePause(queue.queue)}
				disabled={actionLoading === `pause-${queue.queue}`}
				title="Pause"
			>
				{#if actionLoading === `pause-${queue.queue}`}
					{@render spinner()}
				{:else}
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z" />
					</svg>
				{/if}
			</button>
		{/if}
		<button
			type="button"
			class="btn btn-sm preset-outlined-error-500"
			onclick={() => confirmDelete(queue.queue)}
			disabled={actionLoading === `delete-${queue.queue}`}
			title="Delete"
		>
			{#if actionLoading === `delete-${queue.queue}`}
				{@render spinner()}
			{:else}
				<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
				</svg>
			{/if}
		</button>
	</div>
{/snippet}

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
