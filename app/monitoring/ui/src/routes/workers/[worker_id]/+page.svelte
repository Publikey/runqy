<script lang="ts">
	import { parseWorkerQueues } from '$lib/utils/workerQueues';
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { base } from '$app/paths';
	import { workersStore } from '$lib/stores/workers';
	import { settings } from '$lib/stores/settings';
	import { getWorkerLogs } from '$lib/api/client';
	import { formatRelativeTime } from '$lib/utils/format';
	import type { Worker, LogLine } from '$lib/api/types';

	let workerId = $derived(decodeURIComponent($page.params.worker_id));

	let worker = $derived(
		$workersStore.workers.find(w => w.worker_id === workerId)
	);

	let logLines = $state<LogLine[]>([]);
	let eventSource: EventSource | null = null;
	let pollInterval: ReturnType<typeof setInterval> | null = null;
	let logContainer: HTMLDivElement | undefined = $state();
	let pinToBottom = $state(true);
	let logsLoading = $state(true);

	// Metrics helpers
	let m = $derived(worker?.metrics);
	let memPercent = $derived(m && m.memory_total_bytes > 0 ? (m.memory_used_bytes / m.memory_total_bytes) * 100 : 0);

	function formatBytes(bytes: number): string {
		if (bytes < 1024) return bytes + ' B';
		if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(0) + ' KB';
		if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
		return (bytes / (1024 * 1024 * 1024)).toFixed(1) + ' GB';
	}

	function barColor(percent: number): string {
		if (percent >= 90) return 'bg-error-500';
		if (percent >= 70) return 'bg-warning-500';
		return 'bg-success-500';
	}

	// Get worker status
	let processingServerIds = $derived(
		new Set(
			$workersStore.servers
				.filter(s => s.active_workers && s.active_workers.length > 0)
				.map(s => s.id)
		)
	);

	let status = $derived.by(() => {
		if (!worker) return 'unknown';
		if (worker.is_stale) return 'stale';
		if (worker.status === 'bootstrapping') return 'bootstrapping';
		if (worker.status === 'stopped') return 'stopped';
		if (worker.status === 'running' && processingServerIds.has(worker.worker_id)) return 'processing';
		if (worker.status === 'running') return 'idle';
		return 'stopped';
	});

	function scrollToBottom() {
		if (logContainer && pinToBottom) {
			logContainer.scrollTop = logContainer.scrollHeight;
		}
	}

	async function loadInitialLogs() {
		try {
			const resp = await getWorkerLogs(workerId);
			logLines = (resp.lines || []).map((lineStr: string) => {
				try {
					return JSON.parse(lineStr) as LogLine;
				} catch {
					return { ts: Date.now(), src: 'stdout', text: lineStr, seq: 0 } as LogLine;
				}
			});
		} catch {
			// No logs available
		}
		logsLoading = false;
		setTimeout(scrollToBottom, 50);
	}

	function connectSSE() {
		const sseUrl = `${base}/api/workers/${encodeURIComponent(workerId)}/logs/stream`;
		eventSource = new EventSource(sseUrl);

		eventSource.onmessage = (event) => {
			try {
				const line = JSON.parse(event.data) as LogLine;
				logLines = [...logLines, line];
				// Cap at 1000 lines in the UI
				if (logLines.length > 1000) {
					logLines = logLines.slice(-1000);
				}
				setTimeout(scrollToBottom, 10);
			} catch {
				// Ignore unparseable lines
			}
		};

		eventSource.onerror = () => {
			// Reconnect after delay
			if (eventSource) {
				eventSource.close();
				eventSource = null;
			}
			setTimeout(connectSSE, 5000);
		};
	}

	function formatLogTime(ts: number): string {
		const d = new Date(ts);
		return d.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
	}

	onMount(() => {
		workersStore.fetch();
		pollInterval = setInterval(() => workersStore.fetch(), $settings.pollInterval * 1000);
		loadInitialLogs();
		connectSSE();
	});

	onDestroy(() => {
		if (pollInterval) clearInterval(pollInterval);
		if (eventSource) {
			eventSource.close();
			eventSource = null;
		}
	});
</script>

<svelte:head>
	<title>{workerId} - Worker Detail - runqy Monitor</title>
</svelte:head>

<div class="rq-page space-y-8">
	<!-- Back button + Header -->
	<div class="flex items-center gap-4">
		<button
			type="button"
			class="rq-btn-ghost"
			onclick={() => goto(`${base}/workers`)}
		>
			<svg class="w-4 h-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
			</svg>
			Back
		</button>
		<div class="flex-1">
			<h1 class="rq-page-title font-mono">{workerId}</h1>
			{#if worker}
				<p class="text-surface-500 text-sm">
					{#if status === 'processing'}
						<span class="badge preset-filled-primary-500 text-xs">Processing</span>
					{:else if status === 'idle'}
						<span class="badge preset-filled-success-500 text-xs">Idle</span>
					{:else if status === 'stale'}
						<span class="badge preset-filled-warning-500 text-xs">Stale</span>
					{:else if status === 'bootstrapping'}
						<span class="badge preset-filled-secondary-500 text-xs">Bootstrapping</span>
					{:else}
						<span class="badge preset-filled-surface-500 text-xs">Stopped</span>
					{/if}
					<span class="ml-2">Concurrency: {worker.concurrency}</span>
					<span class="ml-2">Started: {formatRelativeTime(new Date(worker.started_at * 1000))}</span>
					<span class="ml-2">Last beat: {formatRelativeTime(new Date(worker.last_beat * 1000))}</span>
				</p>
			{/if}
		</div>
	</div>

	{#if worker}
		<!-- Queues -->
		<div class="flex flex-wrap gap-1">
			{#each parseWorkerQueues(worker.queues) as queue (queue.name)}
				<span class="badge preset-outlined-primary-500 text-sm">
					{queue.name} <span class="text-surface-400 ml-1">p:{queue.priority}</span>
				</span>
			{/each}
		</div>

		<!-- Metrics -->
		{#if m}
			<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
				<!-- CPU -->
				<div class="rq-card p-5">
					<div class="text-xs text-surface-500 mb-2">CPU</div>
					<div class="text-2xl font-bold mb-2">{m.cpu_percent.toFixed(1)}%</div>
					<div class="rq-progress-track">
						<div class="h-full rounded-full transition-all {barColor(m.cpu_percent)}" style="width: {Math.min(m.cpu_percent, 100)}%"></div>
					</div>
				</div>
				<!-- RAM -->
				<div class="rq-card p-5">
					<div class="text-xs text-surface-500 mb-2">Memory</div>
					<div class="text-2xl font-bold mb-2">{formatBytes(m.memory_used_bytes)} <span class="text-sm font-normal text-surface-500">/ {formatBytes(m.memory_total_bytes)}</span></div>
					<div class="rq-progress-track">
						<div class="h-full rounded-full transition-all {barColor(memPercent)}" style="width: {Math.min(memPercent, 100)}%"></div>
					</div>
				</div>
				<!-- GPUs -->
				{#if m.gpus && m.gpus.length > 0}
					{#each m.gpus as gpu (gpu.index)}
						{@const gpuMemPercent = gpu.memory_total_mb > 0 ? (gpu.memory_used_mb / gpu.memory_total_mb) * 100 : 0}
						<div class="rq-card p-5">
							<div class="text-xs text-surface-500 mb-2">GPU {gpu.index} - {gpu.name}</div>
							<div class="text-2xl font-bold mb-1">{gpu.utilization_percent.toFixed(0)}%</div>
							<div class="rq-progress-track mb-2">
								<div class="h-full rounded-full transition-all {barColor(gpu.utilization_percent)}" style="width: {Math.min(gpu.utilization_percent, 100)}%"></div>
							</div>
							<div class="flex justify-between text-xs text-surface-500">
								<span>VRAM: {gpu.memory_used_mb}/{gpu.memory_total_mb} MB ({gpuMemPercent.toFixed(0)}%)</span>
								<span>{gpu.temperature_c}&deg;C</span>
							</div>
						</div>
					{/each}
				{/if}
			</div>
		{/if}

		<!-- Log Viewer -->
		<div class="rq-card">
			<div class="flex items-center justify-between p-4" style="border-bottom: 1px solid var(--runqy-border-subtle);">
				<h2 class="text-lg font-semibold">Logs</h2>
				<div class="flex items-center gap-2">
					<label class="flex items-center gap-2 text-sm text-surface-500">
						<input type="checkbox" bind:checked={pinToBottom} class="checkbox" />
						Auto-scroll
					</label>
				</div>
			</div>
			<div
				bind:this={logContainer}
				class="h-96 overflow-y-auto font-mono text-xs bg-surface-900 text-surface-100 p-4"
			>
				{#if logsLoading}
					<div class="text-surface-400 animate-pulse">Loading logs...</div>
				{:else if logLines.length === 0}
					<div class="text-surface-400">No logs available. Logs appear when the worker process outputs to stderr or non-JSON stdout.</div>
				{:else}
					{#each logLines as line (line.seq || line.ts)}
						<div class="flex gap-2 hover:bg-surface-800 py-px">
							<span class="text-surface-500 shrink-0">{formatLogTime(line.ts)}</span>
							<span class="{line.src === 'stderr' ? 'text-error-400' : 'text-surface-400'} shrink-0 w-12">[{line.src}]</span>
							<span class="break-all">{line.text}</span>
						</div>
					{/each}
				{/if}
			</div>
		</div>
	{:else}
		<div class="rq-card rq-empty-state">
			<p class="text-surface-500">Worker not found or no longer connected.</p>
			<button
				type="button"
				class="rq-btn-primary mt-4"
				onclick={() => goto(`${base}/workers`)}
			>
				Back to Workers
			</button>
		</div>
	{/if}
</div>
