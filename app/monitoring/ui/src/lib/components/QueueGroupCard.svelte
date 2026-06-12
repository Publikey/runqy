<script lang="ts">
	import type { Worker } from '$lib/api/types';
	import type { QueueGroup } from '$lib/utils/queueGrouping';
	import { parseQueueName } from '$lib/utils/queueGrouping';
	import { formatNumber, formatBytes, formatDuration, truncateId } from '$lib/utils/format';

	interface Props {
		group: QueueGroup;
		workers?: Worker[];
		onQueueClick?: (queueName: string) => void;
	}

	let { group, workers = [], onQueueClick }: Props = $props();

	let expanded = $state(true);

	// Parse worker queues string like "map[queue1:5 queue2:5]" to get queue names
	function parseWorkerQueues(queuesStr: string): string[] {
		const match = queuesStr.match(/map\[(.*)]/);
		if (!match) return [];
		return match[1].split(' ').map(q => q.split(':')[0]).filter(Boolean);
	}

	// Find workers listening on any queue in this group
	let listeningWorkers = $derived(
		workers.filter(w => {
			const workerQueues = parseWorkerQueues(w.queues);
			return group.queues.some(q => workerQueues.includes(q.queue));
		})
	);

	function handleGroupClick() {
		if (group.queues.length === 1) {
			onQueueClick?.(group.queues[0].queue);
		} else {
			expanded = !expanded;
		}
	}

	function handleSubQueueClick(e: MouseEvent, queueName: string) {
		e.stopPropagation();
		onQueueClick?.(queueName);
	}
</script>

<div class="rq-card w-full text-left {group.paused ? 'opacity-75' : ''}">
	<!-- Group Header -->
	<button
		type="button"
		class="w-full p-5 hover:bg-white/[0.02] transition-colors rounded-t-[0.75rem]"
		onclick={handleGroupClick}
	>
		<div class="flex items-start justify-between mb-4">
			<div class="flex items-center gap-3 flex-wrap">
				<h3 class="font-semibold text-lg">{group.name}</h3>
				{#if group.queues.length > 1}
					<span class="badge preset-outlined-surface-500 text-xs">{group.queues.length} sub-queues</span>
				{/if}
				{#if group.paused}
					<span class="badge preset-filled-warning-500 text-xs">Paused</span>
				{:else}
					<span class="badge preset-filled-success-500 text-xs">Running</span>
				{/if}
			</div>
			<div class="flex items-center gap-2">
				<div class="text-right text-xs text-surface-500">
					<div>{formatBytes(group.memory_usage || 0)}</div>
					<div>{formatDuration(group.latency_msec || 0)} latency</div>
				</div>
				{#if group.queues.length > 1}
					<svg
						class="w-5 h-5 text-surface-500 transition-transform {expanded ? 'rotate-180' : ''}"
						fill="none"
						stroke="currentColor"
						viewBox="0 0 24 24"
					>
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
					</svg>
				{/if}
			</div>
		</div>

		<!-- Workers listening on this group -->
		<div class="mb-4">
			{#if listeningWorkers.length === 0}
				<div class="text-xs px-3 py-2 rounded flex items-center gap-2 bg-warning-500/10 text-warning-400 border border-warning-500/20">
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
					</svg>
					<span>No workers listening</span>
				</div>
			{:else}
				<div class="flex items-center gap-2 flex-wrap">
					<span class="text-sm text-surface-500">Workers:</span>
					{#each listeningWorkers.slice(0, 3) as worker (worker.worker_id)}
						<span class="badge preset-outlined-primary-500 text-xs" title={worker.worker_id}>
							{truncateId(worker.worker_id, 12)}
						</span>
					{/each}
					{#if listeningWorkers.length > 3}
						<span class="text-xs text-surface-500">+{listeningWorkers.length - 3} more</span>
					{/if}
				</div>
			{/if}
		</div>

		<!-- Aggregated Stats -->
		<div class="grid grid-cols-3 gap-3 text-center">
			<div class="rq-metric-box">
				<div class="text-lg md:text-xl font-bold text-warning-500">{formatNumber(group.pending)}</div>
				<div class="text-xs text-surface-500 mt-1">Pending</div>
			</div>
			<div class="rq-metric-box">
				<div class="text-lg md:text-xl font-bold text-success-500">{formatNumber(group.active)}</div>
				<div class="text-xs text-surface-500 mt-1">Active</div>
			</div>
			<div class="rq-metric-box">
				<div class="text-lg md:text-xl font-bold text-tertiary-500">{formatNumber(group.completed)}</div>
				<div class="text-xs text-surface-500 mt-1">Completed</div>
			</div>
		</div>

		<div class="grid grid-cols-3 gap-3 text-center mt-4">
			<div class="rq-metric-box">
				<div class="text-lg md:text-xl font-bold text-orange-500">{formatNumber(group.retry)}</div>
				<div class="text-xs text-surface-500 mt-1">Retry</div>
			</div>
			<div class="rq-metric-box">
				<div class="text-lg md:text-xl font-bold text-surface-500">{formatNumber(group.archived)}</div>
				<div class="text-xs text-surface-500 mt-1">Archived</div>
			</div>
			<div class="rq-metric-box">
				<div class="text-lg md:text-xl font-bold text-error-500">{formatNumber(group.failed)}</div>
				<div class="text-xs text-surface-500 mt-1">Failed</div>
			</div>
		</div>
	</button>

	<!-- Sub-queues (expanded) -->
	{#if group.queues.length > 1 && expanded}
		<div style="border-top: 1px solid var(--runqy-border-subtle);">
			{#each group.queues as queue (queue.queue)}
				{@const subqueueName = parseQueueName(queue.queue).subqueue || queue.queue}
				<button
					type="button"
					class="w-full px-4 py-3 pl-8 text-left hover:bg-white/[0.02] transition-colors last:rounded-b-[0.75rem]"
					style="border-bottom: 1px solid var(--runqy-border-subtle);"
					onclick={(e) => handleSubQueueClick(e, queue.queue)}
				>
					<div class="flex items-center justify-between">
						<div class="flex items-center gap-2">
							<span class="font-medium text-sm">{subqueueName}</span>
							{#if queue.paused}
								<span class="badge preset-filled-warning-500 text-xs">Paused</span>
							{/if}
						</div>
						<div class="flex items-center gap-4 text-xs">
							<span class="text-warning-500">{formatNumber(queue.pending)} pending</span>
							<span class="text-success-500">{formatNumber(queue.active)} active</span>
							<span class="text-error-500">{formatNumber(queue.failed || 0)} failed</span>
							<svg class="w-4 h-4 text-surface-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
							</svg>
						</div>
					</div>
				</button>
			{/each}
		</div>
	{/if}
</div>
