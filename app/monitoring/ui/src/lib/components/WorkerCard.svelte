<script lang="ts">
	import { parseWorkerQueues } from '$lib/utils/workerQueues';
	import type { Worker } from '$lib/api/types';
	import { formatRelativeTime, truncateId } from '$lib/utils/format';
	import { workersStore } from '$lib/stores/workers';

	let { worker, onclick }: { worker: Worker; onclick?: () => void } = $props();

	// Check if worker is processing (has active tasks on a server with matching id)
	let isProcessing = $derived(
		$workersStore.servers.some(s =>
			s.id === worker.worker_id && s.active_workers && s.active_workers.length > 0
		)
	);

	// Get worker status: 'processing' | 'idle' | 'stale' | 'bootstrapping' | 'stopped'
	let status = $derived.by(() => {
		if (worker.is_stale) return 'stale';
		if (worker.status === 'bootstrapping') return 'bootstrapping';
		if (worker.status === 'draining' || worker.status === 'drained') return worker.status;
		if (worker.status === 'stopped') return 'stopped';
		if (worker.status === 'running' && isProcessing) return 'processing';
		if (worker.status === 'running') return 'idle';
		return 'stopped'; // fallback for unknown statuses
	});

	let queues = $derived(parseWorkerQueues(worker.queues));
	let startedDate = $derived(new Date(worker.started_at * 1000));
	let lastBeatDate = $derived(new Date(worker.last_beat * 1000));

	// Metrics helpers
	let m = $derived(worker.metrics);
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
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
	class="rq-card p-5 {onclick ? 'rq-card-interactive' : ''}"
	onclick={onclick}
>
	<div class="flex items-start justify-between mb-4">
		<div>
			<h3 class="font-semibold font-mono text-sm" title={worker.worker_id}>
				{truncateId(worker.worker_id, 20)}
			</h3>
			<p class="text-xs text-surface-500 mt-1">
				Concurrency: {worker.concurrency}
			</p>
		</div>
		<div>
			{#if status === 'processing'}
				<span class="badge preset-filled-primary-500 text-xs">Processing</span>
			{:else if status === 'idle'}
				<span class="badge preset-filled-success-500 text-xs">Idle</span>
			{:else if status === 'stale'}
				<span class="badge preset-filled-warning-500 text-xs">Stale</span>
			{:else if status === 'bootstrapping'}
				<span class="badge preset-filled-secondary-500 text-xs">Bootstrapping</span>
			{:else if status === 'draining'}
				<span class="badge preset-filled-warning-500 text-xs">Draining</span>
			{:else if status === 'drained'}
				<span class="badge preset-filled-surface-500 text-xs">Drained</span>
			{:else}
				<span class="badge preset-filled-surface-500 text-xs">Stopped</span>
			{/if}
		</div>
	</div>

	{#if m}
		<div class="mb-4 space-y-2">
			<!-- CPU -->
			<div>
				<div class="flex justify-between text-xs text-surface-500 mb-0.5">
					<span>CPU</span>
					<span>{m.cpu_percent.toFixed(1)}%</span>
				</div>
				<div class="rq-progress-track">
					<div class="rq-progress-bar {barColor(m.cpu_percent)}" style="width: {Math.min(m.cpu_percent, 100)}%"></div>
				</div>
			</div>
			<!-- RAM -->
			<div>
				<div class="flex justify-between text-xs text-surface-500 mb-0.5">
					<span>RAM</span>
					<span>{formatBytes(m.memory_used_bytes)} / {formatBytes(m.memory_total_bytes)}</span>
				</div>
				<div class="rq-progress-track">
					<div class="rq-progress-bar {barColor(memPercent)}" style="width: {Math.min(memPercent, 100)}%"></div>
				</div>
			</div>
			<!-- GPUs -->
			{#if m.gpus && m.gpus.length > 0}
				{#each m.gpus as gpu (gpu.index)}
					{@const gpuMemPercent = gpu.memory_total_mb > 0 ? (gpu.memory_used_mb / gpu.memory_total_mb) * 100 : 0}
					<div>
						<div class="flex justify-between text-xs text-surface-500 mb-0.5">
							<span title={gpu.name}>GPU {gpu.index}</span>
							<span>{gpu.utilization_percent.toFixed(0)}% &middot; {gpu.memory_used_mb}/{gpu.memory_total_mb} MB &middot; {gpu.temperature_c}&deg;C</span>
						</div>
						<div class="rq-progress-track">
							<div class="rq-progress-bar {barColor(gpu.utilization_percent)}" style="width: {Math.min(gpu.utilization_percent, 100)}%"></div>
						</div>
					</div>
				{/each}
			{/if}
		</div>
	{/if}

	<div class="mb-4">
		<div class="text-xs text-surface-500 mb-1">Queues</div>
		<div class="flex flex-wrap gap-1">
			{#each queues as queue (queue.name)}
				<span class="badge preset-outlined-primary-500 text-xs">
					{queue.name}
					{#if queue.priority === 0 && status === 'bootstrapping'}
						<svg class="w-3 h-3 ml-1 animate-spin inline-block" fill="none" viewBox="0 0 24 24">
							<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
							<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
						</svg>
					{:else}
						<span class="text-surface-400 ml-1">p:{queue.priority}</span>
					{/if}
				</span>
			{/each}
		</div>
	</div>

	<div class="pt-4 space-y-2 text-xs text-surface-500" style="border-top: 1px solid var(--runqy-border-subtle);">
		<div class="flex justify-between">
			<span>Started:</span>
			<span>{formatRelativeTime(startedDate)}</span>
		</div>
		<div class="flex justify-between">
			<span>Last heartbeat:</span>
			<span>{formatRelativeTime(lastBeatDate)}</span>
		</div>
	</div>
</div>
