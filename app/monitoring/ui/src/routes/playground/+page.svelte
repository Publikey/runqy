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
	import { PageHeader, Panel, Field, Button, Badge, EmptyState } from '$lib/components/ui';
	import { taskStatus } from '$lib/utils/status';
	import { ChevronRight, Send, RotateCw, History } from '@lucide/svelte';

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

<div class="rq-page space-y-4 md:space-y-6">
	<PageHeader
		title="Playground"
		subtitle="Manually enqueue tasks for testing. Tasks land in pending, exactly like client-enqueued ones."
	/>

	<div class="grid grid-cols-1 lg:grid-cols-3 gap-6 items-start">
		<!-- Compose -->
		<Panel title="New task" class="lg:col-span-2" bodyClass="space-y-4">
			<div class="grid grid-cols-1 md:grid-cols-3 gap-4">
				<Field label="Queue" for="pg-queue" class="md:col-span-2">
					{#snippet aside()}
						<Button variant="ghost" size="sm" onclick={() => (useCustomQueue = !useCustomQueue)}>
							{useCustomQueue ? 'Pick an existing queue' : 'Type a name manually'}
						</Button>
					{/snippet}
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
				</Field>
				<div class="grid grid-cols-2 gap-4">
					<Field label="Count" for="pg-count">
						<input id="pg-count" type="number" min="1" max="100" class="input w-full" bind:value={count} />
					</Field>
					<Field label="Timeout (s)" for="pg-timeout">
						<input
							id="pg-timeout"
							type="number"
							min="0"
							class="input w-full"
							placeholder="default"
							bind:value={timeoutSecs}
						/>
					</Field>
				</div>
			</div>

			<Field
				label="Payload (JSON)"
				for="pg-payload"
				error={payloadError ?? undefined}
			>
				{#snippet aside()}
					{#if inputSchema && inputSchema.length > 0}
						<Button variant="ghost" size="sm" onclick={applySkeleton}>Reset to skeleton</Button>
					{/if}
				{/snippet}
				<textarea
					id="pg-payload"
					rows="12"
					class="textarea w-full p-4 text-sm font-mono leading-relaxed"
					spellcheck="false"
					bind:value={payloadText}
				></textarea>
				{#if !payloadError && missingRequired.length > 0}
					<p class="text-xs text-warning-300">
						Missing required field{missingRequired.length > 1 ? 's' : ''}: {missingRequired.join(', ')}
					</p>
				{/if}
			</Field>

			{#if inputSchema && inputSchema.length > 0}
				<details class="group">
					<summary class="inline-flex cursor-pointer select-none items-center gap-1 text-xs font-medium text-fg-subtle hover:text-fg-muted">
						<ChevronRight size={14} strokeWidth={1.75} class="transition-transform duration-150 group-open:rotate-90" />
						Input schema ({inputSchema.length} fields)
					</summary>
					<div class="mt-2 space-y-1">
						{#each inputSchema as field (field.name)}
							<div class="flex flex-wrap items-baseline gap-x-2 text-xs">
								<span class="font-mono text-fg">{field.name}</span>
								<span class="text-fg-subtle">{field.type?.join(' | ')}</span>
								{#if field.required !== false}
									<span class="text-warning-300">required</span>
								{/if}
								{#if field.default !== undefined}
									<span class="text-fg-subtle font-mono">= {JSON.stringify(field.default)}</span>
								{/if}
								{#if field.description}
									<span class="text-fg-subtle truncate">— {field.description}</span>
								{/if}
							</div>
						{/each}
					</div>
				</details>
			{/if}

			<div class="flex flex-wrap items-center justify-end gap-3">
				{#if count > 1}
					<span class="text-xs text-fg-subtle">{count} copies will be enqueued</span>
				{/if}
				<Button variant="primary" icon={Send} loading={sending} disabled={!canSend} onclick={handleSend}>
					{sending ? 'Sending…' : 'Send to queue'}
				</Button>
			</div>
		</Panel>

		<!-- History -->
		<Panel title="History" flush>
			{#snippet actions()}
				{#if history.length > 0}
					<Button variant="ghost" size="sm" class="rq-btn-danger-hover" onclick={clearHistory}>Clear</Button>
				{/if}
			{/snippet}
			{#if history.length === 0}
				<EmptyState icon={History} title="Nothing sent yet" compact />
			{:else}
				<ul class="divide-y divide-line-subtle max-h-[480px] overflow-y-auto">
					{#each history as entry, i (entry.sentAt + i)}
						<li class="px-4 md:px-5 py-3 space-y-1">
							<div class="flex items-center justify-between gap-2">
								<span class="text-ui font-mono text-fg truncate">{entry.queue}</span>
								<span class="text-xs text-fg-subtle shrink-0">{formatRelativeTime(new Date(entry.sentAt))}</span>
							</div>
							<div class="text-xs font-mono text-fg-muted truncate">{truncate(entry.payload.replace(/\s+/g, ' '), 60)}</div>
							<div class="flex items-center gap-2">
								{#if entry.count > 1}
									<span class="text-xs text-fg-subtle tabular-nums">x{entry.count}</span>
								{/if}
								{#if entry.timeout > 0}
									<span class="text-xs text-fg-subtle tabular-nums">{entry.timeout}s timeout</span>
								{/if}
								<div class="flex-1"></div>
								<Button variant="ghost" size="sm" onclick={() => loadEntry(entry)}>Load</Button>
								<Button variant="ghost" size="sm" icon={RotateCw} onclick={() => handleResend(entry)}>Resend</Button>
							</div>
						</li>
					{/each}
				</ul>
			{/if}
		</Panel>
	</div>

	<!-- Live tracking -->
	{#if tracked.length > 0}
		<Panel
			title="Last batch"
			description={extraSent > 0 ? `Tracking the first ${TRACK_MAX} tasks — ${extraSent} more were enqueued.` : undefined}
			flush
		>
			<ul class="divide-y divide-line-subtle">
				{#each tracked as t (t.id)}
					<li class="px-4 md:px-5 py-3">
						<div class="flex flex-wrap items-center gap-x-3 gap-y-1 min-w-0">
							<a
								href="{base}/queues/{encodeURIComponent(t.queue)}"
								class="text-ui font-mono text-primary-400 hover:text-primary-300 break-all"
							>
								{t.id}
							</a>
							<Badge status={taskStatus(t.task?.state)} />
							<span class="text-xs font-mono text-fg-subtle">{t.queue}</span>
							{#if t.task?.state === 'retry' || t.task?.state === 'archived'}
								<span class="text-xs text-error-300 truncate" title={t.task.error_message}>
									{truncate(t.task.error_message ?? '', 80)}
								</span>
							{/if}
						</div>
						{#if t.task?.state === 'completed' && t.task.result}
							<div class="mt-2">
								<JsonViewer data={t.task.result} collapsed={true} maxHeight="200px" />
							</div>
						{/if}
					</li>
				{/each}
			</ul>
		</Panel>
	{/if}
</div>
