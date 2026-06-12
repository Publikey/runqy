<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { base } from '$app/paths';
	import type { Task, TaskState } from '$lib/api/types';
	import {
		getQueueInfo,
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
	import { formatNumber, formatBytes, formatDuration } from '$lib/utils/format';
	import TaskTable from '$lib/components/TaskTable.svelte';
	import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';

	let qname = $derived(decodeURIComponent($page.params.qname));

	type TabState = 'active' | 'pending' | 'retry' | 'archived' | 'completed';
	const tabs: { label: string; state: TabState }[] = [
		{ label: 'Active', state: 'active' },
		{ label: 'Pending', state: 'pending' },
		{ label: 'Retry', state: 'retry' },
		{ label: 'Archived', state: 'archived' },
		{ label: 'Completed', state: 'completed' }
	];

	let activeTab = $state<TabState>('active');
	let queueInfo = $state<Awaited<ReturnType<typeof getQueueInfo>> | null>(null);
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
		await loadQueueInfo();
		updateCountsFromQueueInfo();
	}

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

	onMount(() => {
		loadData();
		loadTasks();
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

<div class="p-6 space-y-6">
	<!-- Breadcrumb -->
	<nav class="flex items-center gap-2 text-sm">
		<a href="{base}/queues" class="text-surface-500 hover:text-primary-500">Queues</a>
		<span class="text-surface-400">/</span>
		<span class="font-medium">{qname}</span>
	</nav>

	<!-- Header -->
	<div class="flex items-start justify-between">
		<div>
			<div class="flex items-center gap-3">
				<h1 class="text-2xl font-bold">{qname}</h1>
				{#if queueInfo}
					{#if queueInfo.paused}
						<span class="badge preset-filled-warning-500">Paused</span>
					{:else}
						<span class="badge preset-filled-success-500">Running</span>
					{/if}
				{/if}
			</div>
			{#if queueInfo}
				<div class="mt-2 flex items-center gap-4 text-sm text-surface-500">
					<span>Latency: {queueInfo.display_latency}</span>
					<span>Memory: {formatBytes(queueInfo.memory_usage_bytes)}</span>
					{#if queueInfo.groups > 0}
						<span>Groups: {queueInfo.groups}</span>
					{/if}
				</div>
			{/if}
		</div>

		<div class="flex items-center gap-2">
			{#if queueInfo?.paused}
				<button type="button" class="rq-btn-primary" onclick={handleResume}>
					<svg class="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z"
						/>
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
						/>
					</svg>
					Resume
				</button>
			{:else}
				<button type="button" class="btn preset-filled-warning-500" onclick={handlePause}>
					<svg class="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z"
						/>
					</svg>
					Pause
				</button>
			{/if}
			<button type="button" class="btn preset-outlined-surface-500" onclick={loadData}>
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

	<!-- Tabs -->
	<div class="flex items-center gap-1 border-b border-surface-300 dark:border-surface-600">
		{#each tabs as tab}
			<button
				type="button"
				class="px-4 py-2 font-medium text-sm border-b-2 transition-colors {activeTab === tab.state
					? 'border-primary-500 text-primary-500'
					: 'border-transparent text-surface-500 hover:text-surface-900 dark:hover:text-surface-100'}"
				onclick={() => (activeTab = tab.state)}
			>
				{tab.label}
				<span
					class="ml-2 px-2 py-0.5 rounded-full text-xs {activeTab === tab.state
						? 'bg-primary-500 text-white'
						: 'bg-surface-200 dark:bg-surface-700'}"
				>
					{formatNumber(taskCounts[tab.state])}
				</span>
			</button>
		{/each}
	</div>

	<!-- Bulk Actions -->
	{#if selectedIds.size > 0 || taskCounts[activeTab] > 0}
		<div class="flex items-center gap-3 p-3 rounded-lg bg-surface-100 dark:bg-surface-800">
			{#if selectedIds.size > 0}
				<span class="text-sm text-surface-500">{selectedIds.size} selected</span>
				<div class="h-4 w-px bg-surface-300 dark:bg-surface-600"></div>
			{/if}

			{#if canBulkRun}
				<button
					type="button"
					class="btn btn-sm preset-outlined-success-500"
					onclick={selectedIds.size > 0 ? handleBulkRun : handleRunAll}
				>
					{selectedIds.size > 0 ? 'Run Selected' : 'Run All'}
				</button>
			{/if}

			{#if canBulkArchive}
				<button
					type="button"
					class="btn btn-sm preset-outlined-surface-500"
					onclick={selectedIds.size > 0 ? handleBulkArchive : handleArchiveAll}
				>
					{selectedIds.size > 0 ? 'Archive Selected' : 'Archive All'}
				</button>
			{/if}

			{#if canBulkDelete}
				<button
					type="button"
					class="btn btn-sm preset-outlined-error-500"
					onclick={selectedIds.size > 0 ? handleBulkDelete : handleDeleteAll}
				>
					{selectedIds.size > 0 ? 'Delete Selected' : 'Delete All'}
				</button>
			{/if}
		</div>
	{/if}

	<!-- Error State -->
	{#if error}
		<div class="card preset-outlined-error-500 p-4">
			<p class="text-error-500">{error}</p>
		</div>
	{/if}

	<!-- Task Table -->
	<TaskTable
		{tasks}
		taskState={activeTab}
		{loading}
		{selectedIds}
		onselect={handleSelect}
		onselectall={handleSelectAll}
		onaction={handleTaskAction}
	/>

	<!-- Pagination -->
	{#if totalPages > 1}
		<div class="flex items-center justify-between">
			<span class="text-sm text-surface-400">
				Page {currentPage} of {totalPages} &middot; {taskCounts[activeTab]} tasks
			</span>
			<div class="flex items-center gap-2">
				<button
					type="button"
					class="rq-btn-ghost"
					disabled={currentPage <= 1}
					onclick={() => goToPage(currentPage - 1)}
				>
					&larr; Previous
				</button>
				<button
					type="button"
					class="rq-btn-ghost"
					disabled={currentPage >= totalPages}
					onclick={() => goToPage(currentPage + 1)}
				>
					Next &rarr;
				</button>
			</div>
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
