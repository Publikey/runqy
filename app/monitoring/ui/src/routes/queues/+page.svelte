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
	import { Callout, PageHeader, Panel, Button, IconButton, Badge, Segmented, EmptyState, Shimmer } from '$lib/components/ui';
	import { queueStatus } from '$lib/utils/status';
	import { Plus, Search, Pause, Play, Trash2, Settings2, Layers, TriangleAlert } from '@lucide/svelte';
	import type { Queue, Worker, QueueConfigDetail, DeploymentConfig } from '$lib/api/types';

	let queueConfigMap = $state<Map<string, QueueConfigDetail>>(new Map());
	let pollInterval: ReturnType<typeof setInterval> | null = null;
	let searchQuery = $state('');
	let statusFilter = $state<'all' | 'running' | 'paused'>('all');
	let showPaused: boolean | null = $derived(
		statusFilter === 'all' ? null : statusFilter === 'paused'
	);
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

<div class="rq-page space-y-6">
	<PageHeader
		title="Queues"
		subtitle="{queueGroups.length} queue{queueGroups.length !== 1 ? 's' : ''} · {subQueueCount} sub-queue{subQueueCount !== 1 ? 's' : ''}"
		onrefresh={loadData}
	>
		{#snippet actions()}
			<Button variant="primary" icon={Plus} onclick={openCreateQueueConfig}>Create queue</Button>
		{/snippet}
	</PageHeader>

	<!-- Filters -->
	<div class="flex flex-wrap items-center gap-3">
		<div class="relative flex-1 min-w-[200px] max-w-md">
			<Search size={16} strokeWidth={1.75} class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-fg-subtle" />
			<input
				type="text"
				value={searchQuery}
				oninput={handleSearchInput}
				placeholder="Search queues..."
				aria-label="Search queues"
				class="input pl-9 w-full"
			/>
		</div>
		<Segmented
			label="Filter by status"
			items={[
				{ value: 'all', label: 'All' },
				{ value: 'running', label: 'Running' },
				{ value: 'paused', label: 'Paused' }
			]}
			bind:value={statusFilter}
		/>
	</div>

	{#if $queuesStore.error}
		<Callout>
			Failed to load queues: {$queuesStore.error}
		</Callout>
	{/if}

	{#if $queuesStore.loading && filteredQueues.length === 0}
		<Panel>
			<Shimmer lines={5} />
		</Panel>
	{:else if filteredQueues.length === 0}
		<Panel>
			<EmptyState
				icon={Layers}
				title={searchQuery ? 'No queues match your search' : 'No queues found'}
				compact
			/>
		</Panel>
	{:else}
		<!-- Desktop: one table, a block per parent queue (header, sub-queues, total) -->
		<Panel flush class="hidden md:block overflow-hidden">
			<div class="table-container">
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
							<th class="text-right"><span class="sr-only">Actions</span></th>
						</tr>
					</thead>
					{#each queueGroups as group (group.name)}
						{@const multi = group.queues.length > 1}
						<tbody>
							<tr class="bg-raised">
								<td colspan="9">
									{@render groupTitle(group)}
								</td>
								<td>
									<div class="flex justify-end">{@render editButton(group)}</div>
								</td>
							</tr>
							{#each group.queues as queue (queue.queue)}
								<tr class="cursor-pointer" onclick={() => navigateToQueue(queue.queue)}>
									<td>
										<div class="pl-4 flex items-center gap-2">
											<span class="font-mono text-fg">{subName(queue)}</span>
											<span class="text-2xs text-fg-faint tabular-nums">p:{priorityOf(queue.queue)}</span>
										</div>
									</td>
									<td><Badge status={queueStatus(queue.paused)} /></td>
									<td>{@render workersBadge(getWorkersForQueue(queue.queue))}</td>
									{@render statCells(queue.pending, queue.active, queue.retry, queue.completed, queue.failed ?? 0)}
									<td class="text-right tabular-nums text-fg-muted">{formatDuration(queue.latency_msec ?? 0)}</td>
									<td>
										<div class="flex justify-end">{@render queueActions(queue)}</div>
									</td>
								</tr>
							{/each}
							{#if multi}
								<tr class="text-fg font-medium">
									<td><div class="pl-4 text-xs font-medium text-fg-subtle">Total</div></td>
									<td>{@render groupStatus(group)}</td>
									<td>{@render workersBadge(getWorkersForGroup(group))}</td>
									{@render statCells(group.pending, group.active, group.retry, group.completed, group.failed)}
									<td class="text-right tabular-nums text-fg-muted" title="Highest sub-queue latency">{formatDuration(group.latency_msec)}</td>
									<td></td>
								</tr>
							{/if}
						</tbody>
					{/each}
				</table>
			</div>
		</Panel>

		<!-- Mobile: the same list as stacked blocks, no horizontal scroll -->
		<div class="md:hidden space-y-4">
			{#each queueGroups as group (group.name)}
				{@const multi = group.queues.length > 1}
				<div class="rounded-lg border border-line bg-panel overflow-hidden">
					<div class="flex items-center justify-between gap-2 px-4 py-3 bg-raised">
						{@render groupTitle(group)}
						{@render editButton(group)}
					</div>
					{#each group.queues as queue (queue.queue)}
						<!-- svelte-ignore a11y_click_events_have_key_events -->
						<!-- svelte-ignore a11y_no_static_element_interactions -->
						<div
							class="px-4 py-3 cursor-pointer border-t border-line-subtle hover:bg-hover transition-colors"
							onclick={() => navigateToQueue(queue.queue)}
						>
							<div class="flex items-center justify-between gap-2">
								<div class="flex items-center gap-2 min-w-0">
									<span class="font-mono text-ui text-fg truncate">{subName(queue)}</span>
									<span class="text-2xs text-fg-faint tabular-nums">p:{priorityOf(queue.queue)}</span>
									<Badge status={queueStatus(queue.paused)} />
								</div>
								{@render queueActions(queue)}
							</div>
							<div class="mt-2">{@render workersBadge(getWorkersForQueue(queue.queue))}</div>
							{@render mobileStats(queue.pending, queue.active, queue.retry, queue.completed, queue.failed ?? 0, queue.latency_msec ?? 0)}
						</div>
					{/each}
					{#if multi}
						<div class="px-4 py-3 border-t border-line">
							<div class="flex items-center justify-between gap-2">
								<span class="text-xs font-medium text-fg-subtle">Total</span>
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
		<span class="font-mono font-semibold text-fg truncate">{group.name}</span>
		{#if group.queues.length > 1}
			<Badge tone="outline">{group.queues.length} sub-queues</Badge>
		{/if}
		{#if redis}
			<Badge tone="outline" title="Results stored in Redis">Redis storage</Badge>
		{/if}
	</div>
{/snippet}

{#snippet editButton(group: QueueGroup)}
	<IconButton
		icon={Settings2}
		label="Edit queue config"
		onclick={() => openEditQueueConfig(group.queues[0].queue)}
	/>
{/snippet}

{#snippet groupStatus(group: QueueGroup)}
	{@const paused = group.queues.filter((q) => q.paused).length}
	{#if paused === 0}
		<Badge status={queueStatus(false)} />
	{:else if paused === group.queues.length}
		<Badge status={queueStatus(true)} />
	{:else}
		<Badge tone="warning">{paused}/{group.queues.length} paused</Badge>
	{/if}
{/snippet}

{#snippet workersBadge(ws: Worker[])}
	{#if ws.length === 0}
		<Badge tone="warning" title={workersTitle(ws)}>
			<TriangleAlert size={12} strokeWidth={1.75} />
			No worker
		</Badge>
	{:else}
		<Badge tone="outline" title={workersTitle(ws)}>
			{ws.length} worker{ws.length !== 1 ? 's' : ''}
		</Badge>
	{/if}
{/snippet}

{#snippet statCells(pending: number, active: number, retry: number, completed: number, failed: number)}
	<td class="text-right tabular-nums {pending > 0 ? 'text-warning-300' : 'text-fg-muted'}">{formatNumber(pending)}</td>
	<td class="text-right tabular-nums text-fg-muted">{formatNumber(active)}</td>
	<td class="text-right tabular-nums text-fg-muted">{formatNumber(retry)}</td>
	<td class="text-right tabular-nums text-fg-muted">{formatNumber(completed)}</td>
	<td class="text-right tabular-nums {failed > 0 ? 'text-error-300' : 'text-fg-muted'}">{formatNumber(failed)}</td>
{/snippet}

{#snippet mobileStats(pending: number, active: number, retry: number, completed: number, failed: number, latency: number)}
	<dl class="grid grid-cols-3 gap-x-3 gap-y-2 mt-3 text-center">
		{@render mobileStat('Pending', formatNumber(pending), pending > 0 ? 'text-warning-300' : 'text-fg-muted')}
		{@render mobileStat('Active', formatNumber(active), 'text-fg-muted')}
		{@render mobileStat('Retry', formatNumber(retry), 'text-fg-muted')}
		{@render mobileStat('Completed', formatNumber(completed), 'text-fg-muted')}
		{@render mobileStat('Failed', formatNumber(failed), failed > 0 ? 'text-error-300' : 'text-fg-muted')}
		{@render mobileStat('Latency', formatDuration(latency), 'text-fg-muted')}
	</dl>
{/snippet}

{#snippet mobileStat(label: string, value: string, tone: string)}
	<div class="flex flex-col-reverse">
		<dt class="text-2xs text-fg-subtle">{label}</dt>
		<dd class="text-ui tabular-nums {tone}">{value}</dd>
	</div>
{/snippet}

{#snippet queueActions(queue: Queue)}
	<div class="flex items-center gap-1 shrink-0">
		{#if queue.paused}
			<IconButton
				icon={Play}
				label="Resume"
				onclick={() => handleResume(queue.queue)}
				loading={actionLoading === `resume-${queue.queue}`}
			/>
		{:else}
			<IconButton
				icon={Pause}
				label="Pause"
				onclick={() => handlePause(queue.queue)}
				loading={actionLoading === `pause-${queue.queue}`}
			/>
		{/if}
		<IconButton
			icon={Trash2}
			label="Delete"
			danger
			onclick={() => confirmDelete(queue.queue)}
			loading={actionLoading === `delete-${queue.queue}`}
		/>
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
