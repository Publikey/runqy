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
	import { queueStatus, countClass } from '$lib/utils/status';
	import { Plus, Search, Pause, Play, Trash2, Settings2, Layers, TriangleAlert, ChevronRight } from '@lucide/svelte';
	import type { Queue, Worker, QueueConfigDetail, DeploymentConfig } from '$lib/api/types';

	let queueConfigMap = $state<Map<string, QueueConfigDetail>>(new Map());
	let pollInterval: ReturnType<typeof setInterval> | null = null;
	let searchQuery = $state('');
	// Mobile: parent queues whose sub-queues are unfolded.
	let expanded = $state<Set<string>>(new Set());

	function toggleExpanded(name: string) {
		const next = new Set(expanded);
		if (next.has(name)) next.delete(name);
		else next.add(name);
		expanded = next;
	}
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

<div class="rq-page space-y-4 md:space-y-6">
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
											<span class="font-medium text-fg">{subName(queue)}</span>
											<span class="font-mono text-2xs text-fg-faint tabular-nums">p:{priorityOf(queue.queue)}</span>
										</div>
									</td>
									<td>{#if queue.paused}<Badge status={queueStatus(true)} />{/if}</td>
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
									<td>
										{#if getWorkersForGroup(group).length > 0}
											{@render workersBadge(getWorkersForGroup(group))}
										{:else}
											<span class="text-ui text-fg-faint">—</span>
										{/if}
									</td>
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

		<!-- Mobile: one compact row per parent queue; multi sub-queue rows unfold in place. -->
		<div class="md:hidden rounded-lg border border-line bg-panel overflow-hidden divide-y divide-line-subtle">
			{#each queueGroups as group (group.name)}
				{@const multi = group.queues.length > 1}
				{@const open = multi && (expanded.has(group.name) || searchQuery !== '')}
				{@const groupWorkers = getWorkersForGroup(group)}
				<div>
					<button
						type="button"
						class="w-full text-left px-3.5 py-3 flex items-center gap-2.5 hover:bg-hover transition-colors"
						aria-expanded={multi ? open : undefined}
						onclick={() => (multi ? toggleExpanded(group.name) : navigateToQueue(group.queues[0].queue))}
					>
						<ChevronRight
							size={14}
							strokeWidth={1.75}
							class="shrink-0 text-fg-subtle transition-transform duration-150 {open ? 'rotate-90' : ''} {multi ? '' : 'invisible'}"
						/>
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-2 min-w-0">
								<span class="text-ui font-medium text-fg truncate">{group.name}</span>
								{#if multi}<span class="text-2xs text-fg-subtle tabular-nums shrink-0">{group.queues.length}</span>{/if}
								{@render compactStatus(group.queues.filter((q) => q.paused).length, group.queues.length)}
							</div>
							{@render compactMeta(group.pending, group.active, group.failed, group.latency_msec, groupWorkers.length)}
						</div>
					</button>
					{#if open}
						<div class="bg-canvas/60 border-t border-line-subtle">
							{#each group.queues as queue (queue.queue)}
								<div class="flex items-center gap-2 pl-9 pr-2 py-2 border-b border-line-subtle last:border-b-0">
									<button type="button" class="min-w-0 flex-1 text-left" onclick={() => navigateToQueue(queue.queue)}>
										<div class="flex items-center gap-2 min-w-0">
											<span class="text-ui font-medium text-fg truncate">{subName(queue)}</span>
											<span class="font-mono text-2xs text-fg-faint tabular-nums shrink-0">p:{priorityOf(queue.queue)}</span>
											{@render compactStatus(queue.paused ? 1 : 0, 1)}
										</div>
										{@render compactMeta(queue.pending, queue.active, queue.failed ?? 0, queue.latency_msec ?? 0, getWorkersForQueue(queue.queue).length)}
									</button>
									{@render queueActions(queue)}
								</div>
							{/each}
							<div class="flex justify-end px-2 py-1.5 border-t border-line-subtle">
								<Button variant="ghost" size="sm" icon={Settings2} onclick={() => openEditQueueConfig(group.queues[0].queue)}>
									Edit config
								</Button>
							</div>
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
		<span class="font-semibold text-fg truncate">{group.name}</span>
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
	{#if paused > 0 && paused === group.queues.length}
		<Badge status={queueStatus(true)} />
	{:else if paused > 0}
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
		<span class="text-ui text-fg-muted tabular-nums" title={workersTitle(ws)}>
			{ws.length} worker{ws.length !== 1 ? 's' : ''}
		</span>
	{/if}
{/snippet}

{#snippet statCells(pending: number, active: number, retry: number, completed: number, failed: number)}
	<td class="text-right tabular-nums {countClass(pending)}">{formatNumber(pending)}</td>
	<td class="text-right tabular-nums {countClass(active)}">{formatNumber(active)}</td>
	<td class="text-right tabular-nums {countClass(retry)}">{formatNumber(retry)}</td>
	<td class="text-right tabular-nums {countClass(completed)}">{formatNumber(completed)}</td>
	<td class="text-right tabular-nums {countClass(failed, 'danger')}">{formatNumber(failed)}</td>
{/snippet}

{#snippet compactStatus(paused: number, total: number)}
	{#if paused === total}
		<Badge status={queueStatus(true)} class="shrink-0" />
	{:else if paused > 0}
		<Badge tone="warning" class="shrink-0">{paused}/{total} paused</Badge>
	{/if}
{/snippet}

{#snippet compactMeta(pending: number, active: number, failed: number, latency: number, workers: number)}
	<div class="mt-0.5 flex flex-wrap items-center gap-x-2.5 gap-y-0.5 text-xs tabular-nums text-fg-subtle">
		<span><span class={countClass(pending)}>{formatNumber(pending)}</span> pending</span>
		<span><span class={countClass(active)}>{formatNumber(active)}</span> active</span>
		{#if failed > 0}<span><span class="text-error-300">{formatNumber(failed)}</span> failed</span>{/if}
		<span>{formatDuration(latency)}</span>
		{#if workers === 0}
			<span class="inline-flex items-center gap-1 text-warning-300"><TriangleAlert size={12} strokeWidth={1.75} />No worker</span>
		{/if}
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
