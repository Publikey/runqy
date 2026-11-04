<script lang="ts">
	import { settings } from '$lib/stores/settings';
	import { onDestroy } from 'svelte';
	import type { Component } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { base } from '$app/paths';
	import type { TaskDetail, TaskState } from '$lib/api/types';
	import {
		getTaskAnyQueue,
		TaskNotFoundError,
		cancelTask,
		runTask,
		archiveTask,
		deleteTask
	} from '$lib/api/client';
	import { toaster } from '$lib/stores/toaster';
	import { formatDateTime, formatRelativeTime, truncateId } from '$lib/utils/format';
	import { taskStatus, type Tone } from '$lib/utils/status';
	import JsonViewer from '$lib/components/JsonViewer.svelte';
	import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
	import {
		PageHeader,
		Panel,
		Badge,
		Button,
		Callout,
		EmptyState,
		KeyValue,
		MoreLink,
		Shimmer,
		Spinner
	} from '$lib/components/ui';
	import type { KV } from '$lib/components/ui/KeyValue.svelte';
	import {
		Check,
		CircleDot,
		Circle,
		Clock,
		RotateCw,
		X,
		Play,
		Archive,
		Trash2,
		SearchX,
		Copy
	} from '@lucide/svelte';

	/** Live follow cadence while the task is not terminal. */
	const FOLLOW_MS = 1500;

	let taskId = $derived(decodeURIComponent($page.params.task_id ?? ''));

	let task = $state<TaskDetail | null>(null);
	let notFound = $state(false);
	let error = $state<string | null>(null);
	let loading = $state(true);
	let busy = $state<string | null>(null);
	let confirmDelete = $state(false);
	let timer: ReturnType<typeof setTimeout> | null = null;
	// Bumped on every task change so answers for a previous id are dropped.
	let generation = 0;

	let terminal = $derived(task?.state === 'completed' || task?.state === 'archived');
	let following = $derived(!!task && !terminal && !notFound);
	let status = $derived(taskStatus(task?.state));

	function isTerminal(s: string | undefined) {
		return s === 'completed' || s === 'archived';
	}

	function stopTimer() {
		if (timer) clearTimeout(timer);
		timer = null;
	}

	function notifyChange(prev: string, next: TaskDetail) {
		const label = taskStatus(next.state).label;
		const description = `${truncateId(next.id, 16)} is now ${label.toLowerCase()}`;
		if (next.state === 'completed') toaster.success({ title: 'Task completed', description });
		else if (next.state === 'archived') toaster.error({ title: 'Task archived', description: next.error_message || description });
		else if (next.state === 'retry') toaster.error({ title: 'Task failed, will retry', description: next.error_message || description });
		else toaster.info({ title: `Task ${label.toLowerCase()}`, description: `${taskStatus(prev).label} → ${label}` });
	}

	async function load() {
		const gen = generation;
		stopTimer();
		try {
			const next = await getTaskAnyQueue(taskId);
			if (gen !== generation) return;
			const prev = task?.state;
			if (prev && prev !== next.state) notifyChange(prev, next);
			task = next;
			notFound = false;
			error = null;
		} catch (e) {
			if (gen !== generation) return;
			if (e instanceof TaskNotFoundError) {
				if (task) toaster.info({ title: 'Task gone', description: 'It was deleted or expired.' });
				notFound = true;
				task = null;
			} else {
				// Transient error (server restart, network): keep the last state and keep trying.
				error = e instanceof Error ? e.message : 'Failed to load the task';
			}
		} finally {
			if (gen === generation) {
				loading = false;
				// Fast while the task moves; then the normal poll interval, so a deletion or
				// expiry still shows up and the Live indicator stays truthful.
				if (!notFound) timer = setTimeout(load, isTerminal(task?.state) ? $settings.pollInterval * 1000 : FOLLOW_MS);
			}
		}
	}

	// (Re)start following whenever the task id changes (navigation between tasks).
	$effect(() => {
		void taskId;
		generation++;
		task = null;
		notFound = false;
		error = null;
		loading = true;
		load();
		return stopTimer;
	});

	onDestroy(stopTimer);

	async function act(action: 'cancel' | 'run' | 'archive' | 'delete') {
		if (!task) return;
		const { queue, id, state } = task;
		busy = action;
		try {
			switch (action) {
				case 'cancel':
					await cancelTask(queue, id);
					toaster.success({ title: 'Cancel requested', description: 'The worker stops the task if it supports cancellation.' });
					break;
				case 'run':
					await runTask(queue, id);
					toaster.success({ title: 'Task moved to pending', description: 'It runs as soon as a worker picks it up.' });
					break;
				case 'archive':
					await archiveTask(queue, id, state);
					toaster.success({ title: 'Task archived' });
					break;
				case 'delete':
					await deleteTask(queue, id, state);
					toaster.success({ title: 'Task deleted' });
					goto(`${base}/tasks`);
					return;
			}
		} catch (e) {
			toaster.error({ title: `Could not ${action} the task`, description: e instanceof Error ? e.message : String(e) });
		} finally {
			busy = null;
		}
		// Resume following (run/archive changes the state; a run task is no longer terminal).
		load();
	}

	// Actions offered per state, matching the per-queue endpoints.
	let canCancel = $derived(task?.state === 'active');
	let canRun = $derived(task?.state === 'scheduled' || task?.state === 'retry' || task?.state === 'archived');
	let canArchive = $derived(task?.state === 'pending' || task?.state === 'scheduled' || task?.state === 'retry');
	let canDelete = $derived(!!task && task.state !== 'active');

	// ---- Timeline ----

	type StepStatus = 'done' | 'current' | 'upcoming';
	interface Step {
		key: string;
		label: string;
		status: StepStatus;
		tone: Tone;
		time?: string;
		timeLabel?: string;
		note?: string;
	}

	function stepsFor(t: TaskDetail): Step[] {
		const s = t.state as TaskState;
		const order = ['scheduled', 'pending', 'active', 'retry', 'final'];
		const currentKey = s === 'completed' || s === 'archived' ? 'final' : s;
		const idx = order.indexOf(currentKey);
		const st = (key: string): StepStatus => {
			const i = order.indexOf(key);
			if (i === idx) return 'current';
			return i < idx ? 'done' : 'upcoming';
		};
		const steps: Step[] = [];
		if (s === 'scheduled') {
			steps.push({ key: 'scheduled', label: 'Scheduled', status: 'current', tone: 'neutral', time: t.next_process_at, timeLabel: 'Runs' });
		}
		steps.push({
			key: 'pending',
			label: 'Pending',
			status: s === 'scheduled' ? 'upcoming' : st('pending'),
			tone: 'neutral',
			time: t.pending_since,
			timeLabel: 'Since'
		});
		steps.push({
			key: 'active',
			label: 'Active',
			status: s === 'scheduled' ? 'upcoming' : st('active'),
			tone: 'accent',
			time: t.worker?.started,
			timeLabel: 'Started',
			note: t.retried > 0 ? `attempt ${t.retried + 1}` : undefined
		});
		if (s === 'retry') {
			steps.push({
				key: 'retry',
				label: 'Retry',
				status: 'current',
				tone: 'warning',
				time: t.next_process_at,
				timeLabel: 'Next attempt',
				note: `${t.retried}/${t.max_retry} retries`
			});
		}
		if (s === 'completed') {
			steps.push({ key: 'final', label: 'Completed', status: 'current', tone: 'success', time: t.completed_at, timeLabel: 'At' });
		} else if (s === 'archived') {
			steps.push({ key: 'final', label: 'Archived', status: 'current', tone: 'danger', time: t.last_failed_at, timeLabel: 'Failed' });
		} else {
			steps.push({ key: 'final', label: 'Completed / Archived', status: 'upcoming', tone: 'neutral' });
		}
		return steps;
	}

	let steps = $derived(task ? stepsFor(task) : []);

	const markerTone: Record<Tone, string> = {
		neutral: 'border-line-strong text-fg bg-raised',
		accent: 'border-primary-500/60 text-primary-300 bg-primary-500/10',
		success: 'border-success-500/60 text-success-300 bg-success-500/10',
		warning: 'border-warning-500/60 text-warning-300 bg-warning-500/10',
		danger: 'border-error-500/60 text-error-300 bg-error-500/10'
	};

	function markerClass(step: Step): string {
		if (step.status === 'current') return markerTone[step.tone];
		if (step.status === 'done') return 'border-line text-fg-muted bg-raised';
		return 'border-line-subtle text-fg-faint bg-transparent';
	}

	function markerIcon(step: Step): Component<any> {
		if (step.status === 'done') return Check;
		if (step.status === 'upcoming') return Circle;
		if (step.key === 'retry') return RotateCw;
		if (step.key === 'scheduled') return Clock;
		if (step.key === 'final') return step.tone === 'danger' ? X : Check;
		return CircleDot;
	}

	// ---- Formatting ----

	function fmtSecs(n: number | undefined | null): string | undefined {
		if (n === undefined || n === null) return undefined;
		if (n <= 0) return 'none';
		const d = Math.floor(n / 86400);
		const h = Math.floor((n % 86400) / 3600);
		const m = Math.floor((n % 3600) / 60);
		const s = n % 60;
		const parts = [d && `${d}d`, h && `${h}h`, m && `${m}m`, s && `${s}s`].filter(Boolean);
		return parts.slice(0, 2).join(' ');
	}

	function fmtTime(t: string | undefined): string | undefined {
		if (!t) return undefined;
		const d = new Date(t);
		if (isNaN(d.getTime()) || d.getFullYear() < 2000) return undefined;
		return `${formatDateTime(d)} (${formatRelativeTime(d)})`;
	}

	async function copyId() {
		try {
			await navigator.clipboard.writeText(taskId);
			toaster.success({ title: 'Copied', description: 'Task ID copied to the clipboard.' });
		} catch {
			// clipboard unavailable (insecure context)
		}
	}

	let executionItems = $derived.by((): KV[] => {
		if (!task) return [];
		const t = task;
		const items: KV[] = [];
		if (t.state === 'active') {
			items.push(t.worker ? { label: 'Worker', render: workerLink } : { label: 'Worker', value: 'Not reported by the worker' });
			if (t.worker?.started) items.push({ label: 'Started', value: fmtTime(t.worker.started) });
			if (t.worker?.deadline) items.push({ label: 'Must finish by', value: fmtTime(t.worker.deadline) });
		}
		items.push({ label: 'Timeout', value: t.timeout_seconds > 0 ? fmtSecs(t.timeout_seconds) : 'none' });
		if (t.deadline) items.push({ label: 'Deadline', value: fmtTime(t.deadline) });
		if (t.limits?.active_timeout !== undefined) items.push({ label: 'Active timeout', value: fmtSecs(t.limits.active_timeout) });
		if (t.limits?.pending_timeout !== undefined) items.push({ label: 'Pending timeout', value: fmtSecs(t.limits.pending_timeout) });
		if (t.limits?.ttl_completed !== undefined) items.push({ label: 'TTL completed', value: fmtSecs(t.limits.ttl_completed) });
		if (t.limits?.ttl_archived !== undefined) items.push({ label: 'TTL archived', value: fmtSecs(t.limits.ttl_archived) });
		if (t.key_ttl_seconds !== undefined) items.push({ label: 'Expires in', value: fmtSecs(t.key_ttl_seconds) });
		else if (t.ttl_seconds && t.ttl_seconds > 0) items.push({ label: 'Retained for', value: fmtSecs(t.ttl_seconds) });
		return items;
	});

	let metadataItems = $derived.by((): KV[] => {
		if (!task) return [];
		const t = task;
		return [
			{ label: 'Task ID', value: t.id, mono: true },
			{ label: 'Queue', render: queueLink },
			{ label: 'Type', value: t.type },
			{ label: 'Retries', value: `${t.retried} / ${t.max_retry}` },
			...(t.group ? [{ label: 'Group', value: t.group, mono: true }] : []),
			...(t.is_orphaned ? [{ label: 'Orphaned', value: 'Yes: no worker holds its lease' }] : [])
		];
	});
</script>

<svelte:head>
	<title>{truncateId(taskId, 12)} - Tasks - runqy</title>
</svelte:head>

{#snippet workerLink()}
	{#if task?.worker}
		<a href="{base}/workers/{encodeURIComponent(task.worker.id)}" class="font-mono text-primary-400 hover:text-primary-300">{task.worker.id}</a>
	{/if}
{/snippet}

{#snippet queueLink()}
	{#if task}
		<a href="{base}/queues/{encodeURIComponent(task.queue)}" class="text-primary-400 hover:text-primary-300">{task.queue}</a>
	{/if}
{/snippet}

<div class="rq-page space-y-4 md:space-y-6">
	<PageHeader
		title={taskId}
		mono
		breadcrumbs={[{ label: 'Tasks', href: '/tasks' }, { label: truncateId(taskId, 12) }]}
		onrefresh={load}
	>
		{#snippet titleExtra()}
			{#if task}
				<Badge {status} />
				{#if following}
					<span class="inline-flex items-center gap-1.5 text-xs text-fg-subtle" title="Refreshing every {FOLLOW_MS / 1000}s until the task finishes">
						<Spinner size={12} />Watching…
					</span>
				{/if}
			{/if}
		{/snippet}
		{#snippet actions()}
			{#if task}
				<Button variant="ghost" size="sm" icon={Copy} onclick={copyId}>Copy ID</Button>
				{#if canCancel}
					<Button variant="secondary" icon={X} loading={busy === 'cancel'} onclick={() => act('cancel')}>Cancel</Button>
				{/if}
				{#if canRun}
					<Button variant="secondary" icon={Play} loading={busy === 'run'} onclick={() => act('run')}>Run now</Button>
				{/if}
				{#if canArchive}
					<Button variant="ghost" icon={Archive} loading={busy === 'archive'} onclick={() => act('archive')}>Archive</Button>
				{/if}
				{#if canDelete}
					<Button variant="ghost" icon={Trash2} class="rq-btn-danger-hover" loading={busy === 'delete'} onclick={() => (confirmDelete = true)}>
						Delete
					</Button>
				{/if}
			{/if}
		{/snippet}
	</PageHeader>

	{#if error}
		<Callout tone="warning" title="Connection problem">{error}. Retrying…</Callout>
	{/if}

	{#if notFound}
		<Panel>
			<EmptyState icon={SearchX} title="This task no longer exists" description="Completed tasks expire after their TTL.">
				{#snippet action()}
					<MoreLink href="/tasks" label="All tasks" />
				{/snippet}
			</EmptyState>
		</Panel>
	{:else if loading && !task}
		<Panel><Shimmer lines={4} /></Panel>
		<Panel><Shimmer lines={6} /></Panel>
	{:else if task}
		{@const t = task}
		<Panel title="Lifecycle">
			{#snippet actions()}
				<MoreLink href="/queues/{encodeURIComponent(t.queue)}" label={t.queue} />
			{/snippet}
			<ol class="flex flex-col md:flex-row md:items-start gap-3 md:gap-0">
				{#each steps as step, i (step.key)}
					{@const Icon = markerIcon(step)}
					<li class="flex md:flex-1 md:flex-col items-start gap-3 md:gap-2 min-w-0">
						<div class="flex md:w-full items-center">
							<span
								class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full border {markerClass(step)}"
								aria-hidden="true"
							>
								{#if step.status === 'current' && step.key === 'active'}
									<Spinner size={14} />
								{:else}
									<Icon size={14} strokeWidth={2} />
								{/if}
							</span>
							{#if i < steps.length - 1}
								<span
									class="hidden md:block h-px flex-1 mx-2 {step.status === 'done' ? 'bg-line-strong' : 'bg-line-subtle'}"
									aria-hidden="true"
								></span>
							{/if}
						</div>
						<div class="min-w-0 md:pr-3" aria-current={step.status === 'current' ? 'step' : undefined}>
							<p class="text-ui font-medium {step.status === 'upcoming' ? 'text-fg-faint' : step.status === 'current' ? 'text-fg' : 'text-fg-muted'}">
								{step.label}
								{#if step.note}<span class="ml-1 text-xs font-normal text-fg-subtle tabular-nums">{step.note}</span>{/if}
							</p>
							{#if step.time && fmtTime(step.time)}
								<p class="text-xs text-fg-subtle" title={formatDateTime(step.time)}>
									{step.timeLabel} {formatRelativeTime(step.time)}
								</p>
							{/if}
						</div>
					</li>
				{/each}
			</ol>
		</Panel>

		{#if t.error_message}
			<Callout tone="danger" title={t.state === 'archived' ? 'Failed' : 'Last error'}>
				<p class="whitespace-pre-wrap">{t.error_message}</p>
				{#if t.last_failed_at}
					<p class="mt-1 text-xs opacity-80">{fmtTime(t.last_failed_at)}</p>
				{/if}
			</Callout>
		{/if}

		<div class="grid grid-cols-1 lg:grid-cols-2 gap-4 md:gap-6 items-start">
			<Panel title="Payload">
				<JsonViewer data={t.payload} collapsed={false} maxHeight="420px" />
			</Panel>
			<Panel title="Result">
				{#if t.result}
					<JsonViewer data={t.result} collapsed={false} maxHeight="420px" />
				{:else}
					<p class="text-ui text-fg-subtle">
						{t.state === 'completed' ? 'The task returned no result.' : 'Appears here as soon as the task stores one.'}
					</p>
				{/if}
			</Panel>
		</div>

		<div class="grid grid-cols-1 lg:grid-cols-2 gap-4 md:gap-6 items-start">
			<Panel title="Execution">
				<KeyValue items={executionItems} inline />
			</Panel>
			<Panel title="Metadata">
				<KeyValue items={metadataItems} inline />
			</Panel>
		</div>
	{/if}
</div>

<ConfirmDialog
	bind:open={confirmDelete}
	title="Delete task"
	message="Delete this task from {task?.queue ?? 'its queue'}? This cannot be undone."
	variant="danger"
	confirmText="Delete"
	onconfirm={() => act('delete')}
/>
