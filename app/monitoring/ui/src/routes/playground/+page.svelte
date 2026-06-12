<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { base } from '$app/paths';
	import type { Task, FieldSchema, QueueConfigDetail } from '$lib/api/types';
	import { playgroundEnqueue, getTask, getQueueConfigs } from '$lib/api/client';
	import { queuesStore } from '$lib/stores/queues';
	import { settings } from '$lib/stores/settings';
	import { toaster } from '$lib/stores/toaster';
	import { formatRelativeTime, truncate } from '$lib/utils/format';
	import JsonViewer from '$lib/components/JsonViewer.svelte';

	const HISTORY_KEY = 'runqy-playground-history';
	const HISTORY_MAX = 20;
	const TRACK_MAX = 20;

	interface HistoryEntry {
		queue: string;
		payload: string;
		count: number;
		timeout: number;
		sentAt: string;
	}

	interface TrackedTask {
		id: string;
		queue: string;
		task: Task | null;
	}

	let selectedQueue = $state('');
	let customQueue = $state('');
	let useCustomQueue = $state(false);
	let configMap = $state<Map<string, QueueConfigDetail>>(new Map());
	let payloadText = $state('{\n  \n}');
	let count = $state(1);
	let timeoutSecs = $state<number | null>(null);
	let sending = $state(false);
	let tracked = $state<TrackedTask[]>([]);
	let extraSent = $state(0); // tasks sent beyond the tracking cap
	let history = $state<HistoryEntry[]>([]);
	let pollInterval: ReturnType<typeof setInterval> | null = null;

	let queueName = $derived(useCustomQueue ? customQueue.trim() : selectedQueue);
	let inputSchema = $derived(configMap.get(queueName)?.input ?? null);

	let payloadError = $derived.by(() => {
		try {
			JSON.parse(payloadText);
			return null;
		} catch (e) {
			return e instanceof Error ? e.message : 'Invalid JSON';
		}
	});

	// Required input-schema fields missing from the payload (warning only — the
	// playground endpoint does not enforce the schema, unlike the public /add API).
	let missingRequired = $derived.by(() => {
		if (!inputSchema || payloadError) return [];
		try {
			const parsed = JSON.parse(payloadText);
			if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return [];
			return inputSchema
				.filter((f) => f.required !== false && !(f.name in parsed))
				.map((f) => f.name);
		} catch {
			return [];
		}
	});

	let canSend = $derived(!sending && !payloadError && queueName.length > 0 && count >= 1 && count <= 100);

	const stateBadge: Record<string, string> = {
		pending: 'preset-filled-warning-500',
		active: 'preset-filled-primary-500',
		scheduled: 'preset-outlined-surface-500',
		retry: 'preset-filled-warning-500',
		completed: 'preset-filled-success-500',
		archived: 'preset-filled-error-500'
	};

	function isTerminal(state: string | undefined): boolean {
		return state === 'completed' || state === 'archived';
	}

	function loadHistory() {
		try {
			const stored = localStorage.getItem(HISTORY_KEY);
			if (stored) history = JSON.parse(stored);
		} catch {
			history = [];
		}
	}

	function saveHistory() {
		localStorage.setItem(HISTORY_KEY, JSON.stringify(history));
	}

	function addToHistory(entry: HistoryEntry) {
		// Skip if identical to the most recent entry for the same queue+payload
		const latest = history[0];
		if (latest && latest.queue === entry.queue && latest.payload === entry.payload) {
			history = [entry, ...history.slice(1)];
		} else {
			history = [entry, ...history].slice(0, HISTORY_MAX);
		}
		saveHistory();
	}

	function placeholderFor(types: string[] | undefined): unknown {
		switch (types?.[0]) {
			case 'int':
			case 'integer':
			case 'float':
			case 'number':
				return 0;
			case 'bool':
			case 'boolean':
				return false;
			case 'array':
				return [];
			case 'object':
			case 'map':
				return {};
			default:
				return '';
		}
	}

	// Build a payload skeleton from the queue's input schema: declared defaults,
	// otherwise a type-appropriate placeholder.
	function skeletonFor(schema: FieldSchema[]): string {
		const obj: Record<string, unknown> = {};
		for (const f of schema) {
			obj[f.name] = f.default !== undefined ? f.default : placeholderFor(f.type);
		}
		return JSON.stringify(obj, null, 2);
	}

	function applySkeleton() {
		if (inputSchema && inputSchema.length > 0) {
			payloadText = skeletonFor(inputSchema);
		}
	}

	function restoreLastPayloadFor(queue: string) {
		const entry = history.find((h) => h.queue === queue);
		if (entry) {
			payloadText = entry.payload;
			count = entry.count;
			timeoutSecs = entry.timeout > 0 ? entry.timeout : null;
			return;
		}
		// No history for this queue: pre-fill from its input schema if it has one.
		const schema = configMap.get(queue)?.input;
		if (schema && schema.length > 0) {
			payloadText = skeletonFor(schema);
		}
	}

	function handleQueueSelect(e: Event) {
		selectedQueue = (e.target as HTMLSelectElement).value;
		if (selectedQueue) restoreLastPayloadFor(selectedQueue);
	}

	function loadEntry(entry: HistoryEntry) {
		const known = $queuesStore.queues.some((q) => q.queue === entry.queue);
		useCustomQueue = !known;
		if (known) {
			selectedQueue = entry.queue;
		} else {
			customQueue = entry.queue;
		}
		payloadText = entry.payload;
		count = entry.count;
		timeoutSecs = entry.timeout > 0 ? entry.timeout : null;
	}

	async function send(queue: string, payload: string, sendCount: number, sendTimeout: number) {
		sending = true;
		try {
			const response = await playgroundEnqueue(queue, JSON.parse(payload), sendCount, sendTimeout);
			tracked = response.tasks.slice(0, TRACK_MAX).map((t) => ({ id: t.id, queue: t.queue, task: null }));
			extraSent = Math.max(0, response.count - TRACK_MAX);
			addToHistory({ queue, payload, count: sendCount, timeout: sendTimeout, sentAt: new Date().toISOString() });
			toaster.success({
				title: 'Enqueued',
				description: `${response.count} task${response.count > 1 ? 's' : ''} sent to ${response.tasks[0]?.queue ?? queue}`
			});
			pollTracked();
		} catch (e) {
			toaster.error({
				title: 'Enqueue failed',
				description: e instanceof Error ? e.message : 'Unknown error'
			});
		} finally {
			sending = false;
		}
	}

	function handleSend() {
		if (!canSend) return;
		send(queueName, payloadText, count, timeoutSecs ?? 0);
	}

	function handleResend(entry: HistoryEntry) {
		send(entry.queue, entry.payload, entry.count, entry.timeout);
	}

	async function pollTracked() {
		const pendingTracked = tracked.filter((t) => !isTerminal(t.task?.state));
		if (pendingTracked.length === 0) return;
		const updates = await Promise.all(
			pendingTracked.map(async (t) => {
				try {
					return { id: t.id, task: await getTask(t.queue, t.id) };
				} catch {
					return null; // task gone (deleted/expired) — keep last known state
				}
			})
		);
		const byId = new Map(updates.filter(Boolean).map((u) => [u!.id, u!.task]));
		tracked = tracked.map((t) => (byId.has(t.id) ? { ...t, task: byId.get(t.id)! } : t));
	}

	function clearHistory() {
		history = [];
		saveHistory();
	}

	async function loadConfigs() {
		try {
			const response = await getQueueConfigs();
			configMap = new Map(response.queues.map((q) => [q.name, q]));
		} catch {
			// Configs unavailable (no DB) — playground still works without skeletons.
		}
	}

	onMount(() => {
		queuesStore.fetch();
		loadHistory();
		loadConfigs();
		pollInterval = setInterval(pollTracked, $settings.pollInterval * 1000);
	});

	onDestroy(() => {
		if (pollInterval) clearInterval(pollInterval);
	});
</script>

<svelte:head>
	<title>Playground - runqy Monitor</title>
</svelte:head>

<div class="rq-page space-y-6">
	<div class="rq-page-header">
		<h1 class="rq-page-title">Playground</h1>
		<p class="rq-page-subtitle">Manually enqueue tasks for testing. Tasks land in pending, exactly like client-enqueued ones.</p>
	</div>

	<div class="grid grid-cols-1 lg:grid-cols-3 gap-6 items-start">
		<!-- Compose -->
		<div class="rq-card p-6 lg:col-span-2 space-y-4">
			<h2 class="text-lg font-semibold">New task</h2>

			<div class="grid grid-cols-1 md:grid-cols-3 gap-4">
				<div class="md:col-span-2">
					<label class="text-xs text-surface-400" for="pg-queue">Queue</label>
					{#if useCustomQueue}
						<input
							id="pg-queue"
							type="text"
							class="input w-full font-mono"
							placeholder="my-queue.default"
							bind:value={customQueue}
						/>
					{:else}
						<select id="pg-queue" class="select w-full" value={selectedQueue} onchange={handleQueueSelect}>
							<option value="" disabled>Select a queue…</option>
							{#each $queuesStore.queues as q (q.queue)}
								<option value={q.queue}>{q.queue}</option>
							{/each}
						</select>
					{/if}
					<button
						type="button"
						class="text-xs text-surface-500 hover:text-surface-300 mt-1"
						onclick={() => (useCustomQueue = !useCustomQueue)}
					>
						{useCustomQueue ? '← Pick an existing queue' : 'Or type a queue name manually'}
					</button>
				</div>
				<div class="grid grid-cols-2 gap-4">
					<div>
						<label class="text-xs text-surface-400" for="pg-count">Count</label>
						<input id="pg-count" type="number" min="1" max="100" class="input w-full" bind:value={count} />
					</div>
					<div>
						<label class="text-xs text-surface-400" for="pg-timeout">Timeout (s)</label>
						<input
							id="pg-timeout"
							type="number"
							min="0"
							class="input w-full"
							placeholder="default"
							bind:value={timeoutSecs}
						/>
					</div>
				</div>
			</div>

			<div>
				<div class="flex items-center justify-between">
					<label class="text-xs text-surface-400" for="pg-payload">Payload (JSON)</label>
					{#if inputSchema && inputSchema.length > 0}
						<button
							type="button"
							class="text-xs text-primary-500 hover:text-primary-400"
							onclick={applySkeleton}
						>
							Reset to skeleton
						</button>
					{/if}
				</div>
				<textarea
					id="pg-payload"
					rows="12"
					class="textarea w-full p-4 rounded text-sm font-mono leading-relaxed scrollbar-thin"
					spellcheck="false"
					bind:value={payloadText}
				></textarea>
				{#if payloadError}
					<p class="text-xs text-error-500 mt-1">{payloadError}</p>
				{:else if missingRequired.length > 0}
					<p class="text-xs text-warning-500 mt-1">
						Missing required field{missingRequired.length > 1 ? 's' : ''}: {missingRequired.join(', ')}
					</p>
				{/if}
			</div>

			{#if inputSchema && inputSchema.length > 0}
				<details>
					<summary class="cursor-pointer text-xs text-surface-400 select-none">
						Input schema ({inputSchema.length} fields)
					</summary>
					<div class="mt-2 space-y-1">
						{#each inputSchema as field (field.name)}
							<div class="flex items-baseline gap-2 text-xs">
								<span class="font-mono text-surface-200">{field.name}</span>
								<span class="text-surface-500">{field.type?.join(' | ')}</span>
								{#if field.required !== false}
									<span class="text-warning-500">required</span>
								{/if}
								{#if field.default !== undefined}
									<span class="text-surface-500 font-mono">= {JSON.stringify(field.default)}</span>
								{/if}
								{#if field.description}
									<span class="text-surface-500 truncate">— {field.description}</span>
								{/if}
							</div>
						{/each}
					</div>
				</details>
			{/if}

			<div class="flex items-center justify-end gap-3">
				{#if count > 1}
					<span class="text-xs text-surface-500">{count} copies will be enqueued</span>
				{/if}
				<button type="button" class="rq-btn-primary" disabled={!canSend} onclick={handleSend}>
					{#if sending}
						Sending…
					{:else}
						Send to queue
					{/if}
				</button>
			</div>
		</div>

		<!-- History -->
		<div class="rq-card p-6 space-y-3">
			<div class="flex items-center justify-between">
				<h2 class="text-lg font-semibold">History</h2>
				{#if history.length > 0}
					<button type="button" class="text-xs text-surface-500 hover:text-error-500" onclick={clearHistory}>
						Clear
					</button>
				{/if}
			</div>
			{#if history.length === 0}
				<p class="text-sm text-surface-500">Nothing sent yet.</p>
			{:else}
				<div class="space-y-2 max-h-[480px] overflow-y-auto scrollbar-thin">
					{#each history as entry, i (entry.sentAt + i)}
						<div class="rq-metric-box space-y-1">
							<div class="flex items-center justify-between gap-2">
								<span class="text-sm font-medium truncate">{entry.queue}</span>
								<span class="text-xs text-surface-500 shrink-0">{formatRelativeTime(new Date(entry.sentAt))}</span>
							</div>
							<div class="text-xs font-mono text-surface-400 truncate">{truncate(entry.payload.replace(/\s+/g, ' '), 60)}</div>
							<div class="flex items-center gap-2">
								{#if entry.count > 1}
									<span class="text-xs text-surface-500">x{entry.count}</span>
								{/if}
								{#if entry.timeout > 0}
									<span class="text-xs text-surface-500">{entry.timeout}s timeout</span>
								{/if}
								<div class="flex-1"></div>
								<button type="button" class="text-xs text-primary-500 hover:text-primary-400" onclick={() => loadEntry(entry)}>
									Load
								</button>
								<button type="button" class="text-xs text-primary-500 hover:text-primary-400" onclick={() => handleResend(entry)}>
									Resend
								</button>
							</div>
						</div>
					{/each}
				</div>
			{/if}
		</div>
	</div>

	<!-- Live tracking -->
	{#if tracked.length > 0}
		<div class="rq-card p-6 space-y-3">
			<h2 class="text-lg font-semibold">Last batch</h2>
			{#if extraSent > 0}
				<p class="text-xs text-surface-500">Tracking the first {TRACK_MAX} tasks — {extraSent} more were enqueued.</p>
			{/if}
			<div class="space-y-2">
				{#each tracked as t (t.id)}
					<div class="rq-metric-box">
						<div class="flex items-center gap-3">
							<a href="{base}/queues/{encodeURIComponent(t.queue)}" class="text-sm font-mono text-primary-500 hover:text-primary-400">
								{t.id}
							</a>
							<span class="badge {stateBadge[t.task?.state ?? 'pending']} text-xs">
								{t.task?.state ?? 'pending'}
							</span>
							<span class="text-xs text-surface-500">{t.queue}</span>
							{#if t.task?.state === 'retry' || t.task?.state === 'archived'}
								<span class="text-xs text-error-500 truncate" title={t.task.error_message}>
									{truncate(t.task.error_message ?? '', 80)}
								</span>
							{/if}
						</div>
						{#if t.task?.state === 'completed' && t.task.result}
							<div class="mt-2">
								<JsonViewer data={t.task.result} collapsed={true} maxHeight="200px" />
							</div>
						{/if}
					</div>
				{/each}
			</div>
		</div>
	{/if}
</div>
