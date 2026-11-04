<script lang="ts">
	import { parseWorkerQueues } from '$lib/utils/workerQueues';
	import type { Worker } from '$lib/api/types';
	import { formatBytes, formatRelativeTime, truncateId } from '$lib/utils/format';
	import { workerState, workerStatus } from '$lib/utils/status';
	import { workersStore } from '$lib/stores/workers';
	import { Badge, ProgressBar, Spinner } from '$lib/components/ui';

	let { worker, onclick }: { worker: Worker; onclick?: () => void } = $props();

	let status = $derived(workerState(worker, $workersStore.servers));

	let queues = $derived(parseWorkerQueues(worker.queues));
	let startedDate = $derived(new Date(worker.started_at * 1000));
	let lastBeatDate = $derived(new Date(worker.last_beat * 1000));

	// Metrics helpers
	let m = $derived(worker.metrics);
	let memPercent = $derived(m && m.memory_total_bytes > 0 ? (m.memory_used_bytes / m.memory_total_bytes) * 100 : 0);
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="rq-card p-4 min-w-0 {onclick ? 'rq-card-interactive' : ''}" {onclick}>
	<div class="flex items-start justify-between gap-3 mb-4">
		<div class="min-w-0">
			<h3 class="text-sm font-semibold font-mono text-fg truncate" title={worker.worker_id}>
				{truncateId(worker.worker_id, 20)}
			</h3>
			<p class="text-xs text-fg-subtle mt-0.5">Concurrency {worker.concurrency}</p>
		</div>
		<Badge status={workerStatus(status)} />
	</div>

	{#if m}
		<div class="mb-4 space-y-3">
			<ProgressBar label="CPU" value={m.cpu_percent} detail="{m.cpu_percent.toFixed(1)}%" />
			<ProgressBar
				label="RAM"
				value={memPercent}
				detail="{formatBytes(m.memory_used_bytes)} / {formatBytes(m.memory_total_bytes)}"
			/>
			{#if m.gpus && m.gpus.length > 0}
				{#each m.gpus as gpu (gpu.index)}
					<ProgressBar
						label="GPU {gpu.index}"
						title={gpu.name}
						value={gpu.utilization_percent}
						detail="{gpu.utilization_percent.toFixed(0)}% · {gpu.memory_used_mb}/{gpu.memory_total_mb} MB · {gpu.temperature_c}°C"
					/>
				{/each}
			{/if}
		</div>
	{/if}

	<div class="mb-4">
		<div class="text-xs font-medium text-fg-subtle mb-1.5">Queues</div>
		<div class="flex flex-wrap gap-1">
			{#each queues as queue (queue.name)}
				<Badge tone="outline">
					<span class="font-mono">{queue.name}</span>
					{#if queue.priority === 0 && status === 'bootstrapping'}
						<Spinner size={11} class="ml-1" />
					{:else}
						<span class="text-fg-faint ml-1 tabular-nums">p:{queue.priority}</span>
					{/if}
				</Badge>
			{/each}
		</div>
	</div>

	<div class="pt-3 border-t border-line-subtle space-y-1 text-xs">
		<div class="flex justify-between gap-2">
			<span class="text-fg-subtle">Started</span>
			<span class="text-fg-muted">{formatRelativeTime(startedDate)}</span>
		</div>
		<div class="flex justify-between gap-2">
			<span class="text-fg-subtle">Last heartbeat</span>
			<span class="text-fg-muted">{formatRelativeTime(lastBeatDate)}</span>
		</div>
	</div>
</div>
