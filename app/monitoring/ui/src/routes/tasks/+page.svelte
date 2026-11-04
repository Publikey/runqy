<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { base } from '$app/paths';
	import type { ListableTaskState, TaskDetail } from '$lib/api/types';
	import { listAllTasks, cancelTask, runTask, archiveTask, deleteTask } from '$lib/api/client';
	import { queuesStore } from '$lib/stores/queues';
	import { settings } from '$lib/stores/settings';
	import { toaster } from '$lib/stores/toaster';
	import { formatNumber } from '$lib/utils/format';
	import TaskTable from '$lib/components/TaskTable.svelte';
	import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
	import { PageHeader, Panel, Tabs, Button, Callout } from '$lib/components/ui';
	import { ChevronLeft, ChevronRight, ArrowRight } from '@lucide/svelte';

	const PAGE_SIZE = 20;
	const STATES: { value: ListableTaskState; label: string }[] = [
		{ value: 'active', label: 'Active' },
		{ value: 'pending', label: 'Pending' },
		{ value: 'scheduled', label: 'Scheduled' },
		{ value: 'retry', label: 'Retry' },
		{ value: 'archived', label: 'Archived' },
		{ value: 'completed', label: 'Completed' }
	];

	function initialState(): ListableTaskState {
		const s = $page.url.searchParams.get('state');
		return STATES.some((x) => x.value === s) ? (s as ListableTaskState) : 'active';
	}

	let activeState = $state<ListableTaskState>(initialState());
	let queueFilter = $state($page.url.searchParams.get('queue') ?? '');
	let currentPage = $state(1);
	let tasks = $state<TaskDetail[]>([]);
	let total = $state(0);
	let loading = $state(false);
	let error = $state<string | null>(null);
	let goToId = $state('');
	let pollTimer: ReturnType<typeof setInterval> | null = null;
	// Ignore answers of requests superseded by a newer one (tab/filter/page change).
	let requestSeq = 0;

	let pendingDelete = $state<{ open: boolean; task: TaskDetail | null }>({ open: false, task: null });

	// Tab counts come from the queue list (already polled for the sidebar pages): no extra cost.
	let counts = $derived.by(() => {
		const out: Record<ListableTaskState, number> = {
			active: 0,
			pending: 0,
			scheduled: 0,
			retry: 0,
			archived: 0,
			completed: 0
		};
		for (const q of $queuesStore.queues) {
			if (queueFilter && q.queue !== queueFilter) continue;
			for (const s of STATES) out[s.value] += q[s.value] ?? 0;
		}
		return out;
	});
	let tabItems = $derived(STATES.map((s) => ({ ...s, count: counts[s.value] })));
	let queueNames = $derived([...$queuesStore.queues.map((q) => q.queue)].sort());
	let totalPages = $derived(Math.max(1, Math.ceil(total / PAGE_SIZE)));

	function syncUrl() {
		const params = new URLSearchParams();
		if (activeState !== 'active') params.set('state', activeState);
		if (queueFilter) params.set('queue', queueFilter);
		const qs = params.toString();
		goto(`${base}/tasks${qs ? `?${qs}` : ''}`, { replaceState: true, keepFocus: true, noScroll: true });
	}

	async function loadTasks(silent = false) {
		const seq = ++requestSeq;
		if (!silent) loading = true;
		try {
			const res = await listAllTasks({
				state: activeState,
				queue: queueFilter || undefined,
				page: currentPage,
				size: PAGE_SIZE
			});
			if (seq !== requestSeq) return;
			tasks = res.tasks ?? [];
			total = res.total ?? 0;
			error = null;
		} catch (e) {
			if (seq !== requestSeq) return;
			if (!silent) {
				error = e instanceof Error ? e.message : 'Failed to load tasks';
				tasks = [];
			}
		} finally {
			if (seq === requestSeq) loading = false;
		}
	}

	function refresh() {
		queuesStore.fetch();
		return loadTasks(true);
	}

	function changeState(s: ListableTaskState) {
		activeState = s;
		currentPage = 1;
		syncUrl();
		loadTasks();
	}

	function changeQueue(e: Event) {
		queueFilter = (e.target as HTMLSelectElement).value;
		currentPage = 1;
		syncUrl();
		loadTasks();
	}

	function goToPage(p: number) {
		if (p < 1 || p > totalPages || p === currentPage) return;
		currentPage = p;
		loadTasks();
	}

	function openTask(e: SubmitEvent) {
		e.preventDefault();
		const id = goToId.trim();
		if (id) goto(`${base}/tasks/${encodeURIComponent(id)}`);
	}

	async function runAction(action: string, task: TaskDetail) {
		try {
			switch (action) {
				case 'cancel':
					await cancelTask(task.queue, task.id);
					toaster.success({ title: 'Cancel requested', description: task.id });
					break;
				case 'run':
					await runTask(task.queue, task.id);
					toaster.success({ title: 'Task queued to run', description: task.id });
					break;
				case 'archive':
					await archiveTask(task.queue, task.id, activeState);
					toaster.success({ title: 'Task archived', description: task.id });
					break;
				case 'delete':
					await deleteTask(task.queue, task.id, activeState);
					toaster.success({ title: 'Task deleted', description: task.id });
					break;
			}
		} catch (e) {
			toaster.error({ title: `Could not ${action} the task`, description: e instanceof Error ? e.message : String(e) });
		}
		await refresh();
	}

	function handleAction(detail: { action: string; taskId: string }) {
		const task = tasks.find((t) => t.id === detail.taskId);
		if (!task) return;
		if (detail.action === 'delete') {
			pendingDelete = { open: true, task };
			return;
		}
		runAction(detail.action, task);
	}

	// Clamp the page when the list shrinks (tasks finished or were deleted).
	$effect(() => {
		if (!loading && currentPage > totalPages) goToPage(totalPages);
	});

	onMount(() => {
		queuesStore.fetch();
		loadTasks();
		pollTimer = setInterval(refresh, $settings.pollInterval * 1000);
	});

	onDestroy(() => {
		if (pollTimer) clearInterval(pollTimer);
	});
</script>

<svelte:head>
	<title>Tasks - runqy</title>
</svelte:head>

<div class="rq-page space-y-4 md:space-y-6">
	<PageHeader title="Tasks" subtitle="Tasks of every queue, by state." onrefresh={refresh}>
		{#snippet actions()}
			<form class="flex items-center gap-2 w-full sm:w-auto" onsubmit={openTask}>
				<input
					type="text"
					class="input font-mono w-60 sm:w-72"
					placeholder="Go to task ID…"
					aria-label="Go to task ID"
					bind:value={goToId}
				/>
				<Button type="submit" variant="secondary" icon={ArrowRight} disabled={!goToId.trim()}>Open</Button>
			</form>
		{/snippet}
	</PageHeader>

	<div class="space-y-4">
		<div class="flex flex-wrap items-end justify-between gap-3">
			<div class="min-w-0 max-w-full overflow-x-auto">
				<Tabs items={tabItems} value={activeState} onchange={changeState} />
			</div>
			<select class="select w-full sm:w-56" aria-label="Filter by queue" value={queueFilter} onchange={changeQueue}>
				<option value="">All queues</option>
				{#each queueNames as q (q)}
					<option value={q}>{q}</option>
				{/each}
				{#if queueFilter && !queueNames.includes(queueFilter)}
					<option value={queueFilter}>{queueFilter}</option>
				{/if}
			</select>
		</div>

		{#if error}
			<Callout>{error}</Callout>
		{/if}

		<Panel flush class="overflow-hidden" footer={totalPages > 1 ? pager : undefined}>
			<TaskTable
				{tasks}
				taskState={activeState}
				loading={loading && tasks.length === 0}
				showQueue
				selectable={false}
				onaction={handleAction}
			/>
		</Panel>
	</div>
</div>

{#snippet pager()}
	<div class="flex flex-wrap items-center justify-between gap-2">
		<span class="text-ui text-fg-subtle tabular-nums">
			Page {currentPage} of {totalPages} &middot; {formatNumber(total)} tasks
		</span>
		<div class="flex items-center gap-2">
			<Button variant="ghost" size="sm" icon={ChevronLeft} disabled={currentPage <= 1} onclick={() => goToPage(currentPage - 1)}>
				Previous
			</Button>
			<Button variant="ghost" size="sm" disabled={currentPage >= totalPages} onclick={() => goToPage(currentPage + 1)}>
				Next
				<ChevronRight size={14} strokeWidth={1.75} />
			</Button>
		</div>
	</div>
{/snippet}

<ConfirmDialog
	bind:open={pendingDelete.open}
	title="Delete task"
	message={pendingDelete.task ? `Delete task ${pendingDelete.task.id} from ${pendingDelete.task.queue}? This cannot be undone.` : ''}
	variant="danger"
	confirmText="Delete"
	onconfirm={() => pendingDelete.task && runAction('delete', pendingDelete.task)}
/>
