<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/stores';
	import type { Task, TaskState, QueueConfigDetail } from '$lib/api/types';
	import {
		getQueueInfo,
		getQueueConfig,
		getTasks,
		pauseQueue,
		resumeQueue,
		deleteTask,
		cancelTask,
		runTask,
		archiveTask,
		deleteAllTasks,
		archiveAllTasks,
		runAllRetryTasks,
		runAllArchivedTasks,
		batchDeleteTasks,
		batchArchiveTasks,
		batchRunTasks
	} from '$lib/api/client';
	import { settings } from '$lib/stores/settings';
	import { formatNumber, formatBytes, formatDateTime, formatRelativeTime } from '$lib/utils/format';
	import { base } from '$app/paths';
	import { workersStore } from '$lib/stores/workers';
	import { parseWorkerQueues } from '$lib/utils/workerQueues';
	import { parseQueueName } from '$lib/utils/queueGrouping';
	import TaskTable from '$lib/components/TaskTable.svelte';
	import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
	import { Callout, PageHeader, Panel, Button, Badge, KeyValue, Tabs } from '$lib/components/ui';
	import { queueStatus, workerState, workerStatus } from '$lib/utils/status';
	import { Play, Pause, Archive, Trash2, ChevronLeft, ChevronRight, TriangleAlert } from '@lucide/svelte';

	let qname = $derived(decodeURIComponent($page.params.qname ?? ''));

	type TabState = 'active' | 'pending' | 'retry' | 'archived' | 'completed';
	const tabs: { label: string; state: TabState }[] = [
		{ label: 'Active', state: 'active' },
		{ label: 'Pending', state: 'pending' },
		{ label: 'Retry', state: 'retry' },
		{ label: 'Archived', state: 'archived' },
		{ label: 'Completed', state: 'completed' }
	];

	let activeTab = $state<TabState>('active');
	let tabItems = $derived(
		tabs.map((t) => ({ value: t.state, label: t.label, count: taskCounts[t.state] }))
	);
	let queueInfo = $state<Awaited<ReturnType<typeof getQueueInfo>> | null>(null);
	let queueConfig = $state<QueueConfigDetail | null>(null);
	let configOpen = $state(false);
	let tasks = $state<Task[]>([]);
	let taskCounts = $state<Record<TabState, number>>({
		active: 0,
		pending: 0,
		retry: 0,
		archived: 0,
		completed: 0
	});
	let loading = $state(false);
	let error = $state<string | null>(null);
	let selectedIds = $state(new Set<string>());
	let pollInterval: ReturnType<typeof setInterval> | null = null;

	const PAGE_SIZE = 20;
	let currentPage = $state(1);
	let totalPages = $derived(Math.max(1, Math.ceil((taskCounts[activeTab] ?? 0) / PAGE_SIZE)));

	let confirmDialog = $state({
		open: false,
		title: '',
		message: '',
		action: () => {}
	});

	let headerMeta = $derived(
		queueInfo
			? [
					`Latency ${queueInfo.display_latency}`,
					`Memory ${formatBytes(queueInfo.memory_usage_bytes)}`,
					...(queueInfo.groups > 0 ? [`Groups ${queueInfo.groups}`] : [])
				].join(' · ')
			: undefined
	);

	let canBulkRun = $derived(activeTab === 'retry' || activeTab === 'archived');
	let canBulkArchive = $derived(activeTab === 'pending' || activeTab === 'retry');
	let canBulkDelete = $derived(activeTab !== 'active');

	async function loadQueueInfo() {
		try {
			queueInfo = await getQueueInfo(qname);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load queue info';
		}
	}

	async function loadTasks() {
		loading = true;
		error = null;
		try {
			const response = await getTasks(qname, activeTab, currentPage, PAGE_SIZE);
			tasks = response.tasks || [];
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load tasks';
			tasks = [];
		} finally {
			loading = false;
		}
	}

	function goToPage(page: number) {
		if (page < 1 || page > totalPages || page === currentPage) return;
		currentPage = page;
		selectedIds = new Set();
		loadTasks();
	}

	function updateCountsFromQueueInfo() {
		if (!queueInfo) return;
		taskCounts = {
			active: queueInfo.active ?? 0,
			pending: queueInfo.pending ?? 0,
			retry: queueInfo.retry ?? 0,
			archived: queueInfo.archived ?? 0,
			completed: queueInfo.completed ?? 0
		};
	}

	async function loadData() {
		await Promise.all([loadQueueInfo(), workersStore.fetch()]);
		updateCountsFromQueueInfo();
	}

	// Workers listening on this sub-queue, with what they are doing on it.
	let queueWorkers = $derived.by(() => {
		const parent = parseQueueName(qname).parent;
		return $workersStore.workers
			.map((w) => {
				const entry = parseWorkerQueues(w.queues).find((q) => q.name === qname);
				if (!entry) return null;
				const server = $workersStore.servers.find((s) => s.id === w.worker_id);
				const running = (server?.active_workers ?? []).filter((a) => a.queue === qname).length;
				return {
					worker: w,
					priority: entry.priority,
					running,
					held: (w.held ?? []).includes(parent),
					status: workerStatus(workerState(w, $workersStore.servers))
				};
			})
			.filter((x) => x !== null)
			.sort((a, b) => a.worker.worker_id.localeCompare(b.worker.worker_id));
	});

	async function pollData() {
		await loadData();
		// Silently refresh the current page without showing loading skeleton
		try {
			const response = await getTasks(qname, activeTab, currentPage, PAGE_SIZE);
			tasks = response.tasks || [];
		} catch {
			// Silently ignore poll errors to avoid flashing error messages
		}
	}

	async function loadQueueConfig() {
		try {
			queueConfig = await getQueueConfig(qname);
		} catch {
			// No config for this queue (e.g. created directly in Redis) — hide the panel.
			queueConfig = null;
		}
	}

	onMount(() => {
		loadData();
		loadTasks();
		loadQueueConfig();
		pollInterval = setInterval(pollData, $settings.pollInterval * 1000);
	});

	onDestroy(() => {
		if (pollInterval) clearInterval(pollInterval);
	});

	// Reload tasks when tab changes
	let previousTab = $state<TabState | null>(null);
	$effect(() => {
		if (activeTab && activeTab !== previousTab) {
			previousTab = activeTab;
			currentPage = 1;
			loadTasks();
			selectedIds = new Set();
		}
	});

	// Clamp the page if the task count shrinks below the current page (e.g. bulk delete).
	$effect(() => {
		if (currentPage > totalPages) {
			goToPage(totalPages);
		}
	});

	async function handlePause() {
		try {
			await pauseQueue(qname);
			await loadQueueInfo();
		} catch (e) {
			console.error('Failed to pause queue:', e);
		}
	}

	async function handleResume() {
		try {
			await resumeQueue(qname);
			await loadQueueInfo();
		} catch (e) {
			console.error('Failed to resume queue:', e);
		}
	}

	function handleSelect(detail: { id: string; selected: boolean }) {
		if (detail.selected) {
			selectedIds.add(detail.id);
		} else {
			selectedIds.delete(detail.id);
		}
		selectedIds = new Set(selectedIds); // Trigger reactivity
	}

	function handleSelectAll(detail: { selected: boolean }) {
		if (detail.selected) {
			selectedIds = new Set(tasks.map((t) => t.id));
		} else {
			selectedIds = new Set();
		}
	}

	async function handleTaskAction(detail: { action: string; taskId: string }) {
		const { action, taskId } = detail;
		try {
			switch (action) {
				case 'cancel':
					await cancelTask(qname, taskId);
					break;
				case 'run':
					await runTask(qname, taskId);
					break;
				case 'archive':
					await archiveTask(qname, taskId, activeTab);
					break;
				case 'delete':
					await deleteTask(qname, taskId, activeTab);
					break;
			}
			await loadTasks();
			await loadData();
		} catch (e) {
			console.error(`Failed to ${action} task:`, e);
		}
	}

	// Bulk actions
	async function handleBulkDelete() {
		if (selectedIds.size === 0) return;
		confirmDialog = {
			open: true,
			title: 'Delete Tasks',
			message: `Are you sure you want to delete ${selectedIds.size} task(s)?`,
			action: async () => {
				try {
					await batchDeleteTasks(qname, Array.from(selectedIds), activeTab);
					selectedIds = new Set();
					await loadTasks();
					await loadData();
				} catch (e) {
					console.error('Failed to delete tasks:', e);
				}
			}
		};
	}

	async function handleBulkArchive() {
		if (selectedIds.size === 0) return;
		try {
			await batchArchiveTasks(qname, Array.from(selectedIds), activeTab);
			selectedIds = new Set();
			await loadTasks();
			await loadData();
		} catch (e) {
			console.error('Failed to archive tasks:', e);
		}
	}

	async function handleBulkRun() {
		if (selectedIds.size === 0) return;
		try {
			await batchRunTasks(qname, Array.from(selectedIds));
			selectedIds = new Set();
			await loadTasks();
			await loadData();
		} catch (e) {
			console.error('Failed to run tasks:', e);
		}
	}

	// Bulk all actions
	function handleDeleteAll() {
		confirmDialog = {
			open: true,
			title: 'Delete All Tasks',
			message: `Are you sure you want to delete all ${taskCounts[activeTab]} ${activeTab} task(s)?`,
			action: async () => {
				try {
					await deleteAllTasks(qname, activeTab);
					await loadTasks();
					await loadData();
				} catch (e) {
					console.error('Failed to delete all tasks:', e);
				}
			}
		};
	}

	async function handleArchiveAll() {
		try {
			await archiveAllTasks(qname, activeTab);
			await loadTasks();
			await loadData();
		} catch (e) {
			console.error('Failed to archive all tasks:', e);
		}
	}

	async function handleRunAll() {
		try {
			if (activeTab === 'retry') {
				await runAllRetryTasks(qname);
			} else if (activeTab === 'archived') {
				await runAllArchivedTasks(qname);
			}
			await loadTasks();
			await loadData();
		} catch (e) {
			console.error('Failed to run all tasks:', e);
		}
	}
</script>

<svelte:head>
	<title>{qname} - Queues - runqy</title>
</svelte:head>

<div class="rq-page space-y-4 md:space-y-6">
	<PageHeader
		title={qname}
		mono
		breadcrumbs={[{ label: 'Queues', href: '/queues' }, { label: qname }]}
		subtitle={headerMeta}
		onrefresh={loadData}
	>
		{#snippet titleExtra()}
			{#if queueInfo}
				<Badge status={queueStatus(queueInfo.paused)} />
			{/if}
		{/snippet}
		{#snippet actions()}
			{#if queueInfo?.paused}
				<Button variant="secondary" icon={Play} onclick={handleResume}>Resume</Button>
			{:else}
				<Button variant="secondary" icon={Pause} onclick={handlePause}>Pause</Button>
			{/if}
		{/snippet}
	</PageHeader>

	<!-- Workers serving this sub-queue -->
	<Panel title="Workers" flush>
		{#snippet titleExtra()}
			<Badge tone="outline">{queueWorkers.length}</Badge>
		{/snippet}
		{#if queueWorkers.length === 0}
			<div class="flex items-start gap-2.5 px-4 md:px-5 py-3 text-ui text-warning-200">
				<TriangleAlert size={16} strokeWidth={1.75} class="mt-0.5 shrink-0 text-warning-300" />
				<span>No worker listens on this queue: its tasks stay pending until one starts.</span>
			</div>
		{:else}
			<ul class="divide-y divide-line-subtle">
				{#each queueWorkers as qw (qw.worker.worker_id)}
					<li>
						<a
							href="{base}/workers/{encodeURIComponent(qw.worker.worker_id)}"
							class="flex flex-wrap items-center gap-x-4 gap-y-1 px-4 md:px-5 py-2.5 hover:bg-hover transition-colors"
						>
							<span class="font-mono text-ui text-fg truncate min-w-0 flex-1 basis-40">{qw.worker.worker_id}</span>
							<span class="flex flex-wrap items-center gap-2">
								<Badge status={qw.status} />
								{#if qw.held}<Badge tone="warning" title="Not taking new tasks from this queue (drain or code update)">Held</Badge>{/if}
							</span>
							<span class="text-xs text-fg-subtle tabular-nums" title="Tasks of this queue running on the worker / worker concurrency">
								<span class={qw.running > 0 ? 'text-primary-300' : 'text-fg-muted'}>{qw.running}</span>/{qw.worker.concurrency} running
							</span>
							<span class="text-xs text-fg-subtle tabular-nums">p:{qw.priority}</span>
							<span class="text-xs text-fg-subtle">Heartbeat {formatRelativeTime(new Date(qw.worker.last_beat * 1000))}</span>
						</a>
					</li>
				{/each}
			</ul>
		{/if}
	</Panel>

	<!-- Queue Configuration (read-only, collapsible) -->
	{#if queueConfig}
		{@const cfg = queueConfig}
		<Panel
			title="Configuration"
			flush={!configOpen}
			class="overflow-hidden"
			bodyClass={configOpen ? 'space-y-6' : '-mt-px'}
		>
			{#snippet titleExtra()}
				{#if cfg.deployment?.mode}
					<Badge tone="outline">{cfg.deployment.mode}</Badge>
				{/if}
				<span class="text-xs text-fg-subtle tabular-nums">Priority {cfg.priority}</span>
			{/snippet}
			{#snippet actions()}
				<Button variant="ghost" size="sm" onclick={() => (configOpen = !configOpen)} aria-expanded={configOpen}>
					<ChevronRight
						size={14}
						strokeWidth={1.75}
						class="transition-transform duration-150 {configOpen ? 'rotate-90' : ''}"
					/>
					{configOpen ? 'Hide details' : 'Show details'}
				</Button>
			{/snippet}

			{#if configOpen}
				{#if cfg.deployment}
					{@const d = cfg.deployment}
					{#snippet redisValue()}
						{#if d.redis_storage}
							<Badge tone="success">Enabled</Badge>
						{:else}
							<span class="text-fg-subtle">Disabled</span>
						{/if}
					{/snippet}
					{#snippet vaultsValue()}
						{#if d.vaults?.length}
							<div class="flex items-center gap-1 flex-wrap">
								{#each d.vaults as vault (vault)}
									<Badge tone="outline" class="font-mono">{vault}</Badge>
								{/each}
							</div>
						{:else}
							<span class="text-fg-subtle">None</span>
						{/if}
					{/snippet}
					<div class="space-y-3">
						<h3 class="text-sm font-semibold text-fg">Deployment</h3>
						<KeyValue
							items={[
								{ label: 'Git URL', value: d.git_url || '—', mono: true },
								{ label: 'Branch', value: d.branch || '—', mono: true },
								...(d.code_path ? [{ label: 'Code path', value: d.code_path, mono: true }] : []),
								{ label: 'Startup command', value: d.startup_cmd || '—', mono: true },
								{ label: 'Startup timeout', value: `${d.startup_timeout_secs}s` },
								{ label: 'Redis storage', render: redisValue },
								...(d.git_token ? [{ label: 'Git token', value: d.git_token, mono: true }] : []),
								{ label: 'Vaults', render: vaultsValue }
							]}
						/>
					</div>
					<div class="space-y-3 border-t border-line-subtle pt-5">
						<h3 class="text-sm font-semibold text-fg">Task lifecycle limits</h3>
						<KeyValue
							columns={3}
							items={[
								{ label: 'Max retry', value: d.limits?.max_retry },
								{ label: 'TTL completed', value: d.limits?.ttl_completed },
								{ label: 'TTL archived', value: d.limits?.ttl_archived },
								{ label: 'Pending timeout', value: d.limits?.pending_timeout },
								{ label: 'Active timeout', value: d.limits?.active_timeout }
							].map((l) =>
								l.value !== undefined && l.value !== ''
									? { label: l.label, value: l.value, mono: true }
									: { label: l.label, value: 'server default' }
							)}
						/>
					</div>
				{:else}
					<p class="text-ui text-fg-subtle">No deployment configured.</p>
				{/if}
				<div class="flex flex-wrap items-center gap-x-6 gap-y-1 border-t border-line-subtle pt-4 text-xs text-fg-subtle">
					<span>Created {formatDateTime(new Date(cfg.created_at * 1000))}</span>
					<span>Updated {formatDateTime(new Date(cfg.updated_at * 1000))}</span>
				</div>
			{/if}
		</Panel>
	{/if}

	<div class="space-y-4">
		<Tabs items={tabItems} bind:value={activeTab} />

		<!-- Error State -->
		{#if error}
			<Callout>
				{error}
			</Callout>
		{/if}

		<Panel flush class="overflow-hidden" footer={totalPages > 1 ? pager : undefined}>
			<!-- Bulk Actions -->
			{#if selectedIds.size > 0 || taskCounts[activeTab] > 0}
				<div class="flex flex-wrap items-center gap-2 px-4 md:px-5 py-2.5 border-b border-line-subtle bg-raised">
					{#if selectedIds.size > 0}
						<span class="text-ui text-fg-muted tabular-nums">{selectedIds.size} selected</span>
						<div class="h-4 w-px bg-line" aria-hidden="true"></div>
					{/if}

					{#if canBulkRun}
						<Button
							size="sm"
							variant="secondary"
							icon={Play}
							onclick={selectedIds.size > 0 ? handleBulkRun : handleRunAll}
						>
							{selectedIds.size > 0 ? 'Run Selected' : 'Run All'}
						</Button>
					{/if}

					{#if canBulkArchive}
						<Button
							size="sm"
							variant="ghost"
							icon={Archive}
							onclick={selectedIds.size > 0 ? handleBulkArchive : handleArchiveAll}
						>
							{selectedIds.size > 0 ? 'Archive Selected' : 'Archive All'}
						</Button>
					{/if}

					{#if canBulkDelete}
						<Button
							size="sm"
							variant="ghost"
							icon={Trash2}
							class="rq-btn-danger-hover"
							onclick={selectedIds.size > 0 ? handleBulkDelete : handleDeleteAll}
						>
							{selectedIds.size > 0 ? 'Delete Selected' : 'Delete All'}
						</Button>
					{/if}
				</div>
			{/if}

			<TaskTable
				{tasks}
				taskState={activeTab}
				{loading}
				{selectedIds}
				onselect={handleSelect}
				onselectall={handleSelectAll}
				onaction={handleTaskAction}
			/>

		</Panel>
	</div>
</div>

{#snippet pager()}
		<div class="flex flex-wrap items-center justify-between gap-2">
			<span class="text-ui text-fg-subtle tabular-nums">
				Page {currentPage} of {totalPages} &middot; {formatNumber(taskCounts[activeTab])} tasks
			</span>
			<div class="flex items-center gap-2">
				<Button
					variant="ghost"
					size="sm"
					icon={ChevronLeft}
					disabled={currentPage <= 1}
					onclick={() => goToPage(currentPage - 1)}
				>
					Previous
				</Button>
				<Button
					variant="ghost"
					size="sm"
					disabled={currentPage >= totalPages}
					onclick={() => goToPage(currentPage + 1)}
				>
					Next
					<ChevronRight size={14} strokeWidth={1.75} />
				</Button>
			</div>
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
