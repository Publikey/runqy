<script lang="ts">
	import type { Queue, Worker } from '$lib/api/types';
	import { formatNumber, formatBytes, formatDuration, truncateId } from '$lib/utils/format';

	interface Props {
		queue: Queue;
		workers?: Worker[];
		redisStorage?: boolean;
		onClick?: () => void;
		onPause?: () => void;
		onResume?: () => void;
		onDelete?: () => void;
	}

	let { queue, workers = [], redisStorage = false, onClick, onPause, onResume, onDelete }: Props = $props();

	let menuOpen = $state(false);

	// Parse worker queues string like "map[queue1:5 queue2:5]" to get queue names
	function parseWorkerQueues(queuesStr: string): string[] {
		const match = queuesStr.match(/map\[(.*)]/);
		if (!match) return [];
		return match[1].split(' ').map(q => q.split(':')[0]).filter(Boolean);
	}

	// Find workers listening on this queue
	let listeningWorkers = $derived(
		workers.filter(w => {
			const workerQueues = parseWorkerQueues(w.queues);
			return workerQueues.includes(queue.queue);
		})
	);

	function handleMenuClick(e: MouseEvent) {
		e.stopPropagation();
		menuOpen = !menuOpen;
	}

	function handleAction(action: (() => void) | undefined, e: MouseEvent) {
		e.stopPropagation();
		menuOpen = false;
		if (action) action();
	}

	function handleClickOutside(e: MouseEvent) {
		menuOpen = false;
	}
</script>

<svelte:window onclick={handleClickOutside} />

<div
	class="rq-card rq-card-interactive p-5 w-full text-left {queue.paused ? 'opacity-75' : ''}"
	onclick={onClick}
	onkeydown={(e) => e.key === 'Enter' && onClick?.()}
	role="button"
	tabindex="0"
>
	<div class="flex items-start justify-between mb-4">
		<div class="flex items-center gap-3 flex-wrap">
			<h3 class="font-semibold text-lg">{queue.queue}</h3>
			{#if queue.paused}
				<span class="badge preset-filled-warning-500 text-xs">Paused</span>
			{:else}
				<span class="badge preset-filled-success-500 text-xs">Running</span>
			{/if}
			{#if redisStorage}
				<span class="badge preset-filled-success-500 text-xs">Redis</span>
			{:else}
				<span class="badge preset-filled-surface-500 text-xs">None</span>
			{/if}
		</div>
		<div class="flex items-center gap-2">
			<div class="text-right text-xs text-surface-500">
				<div>{formatBytes(queue.memory_usage || 0)}</div>
				<div>{formatDuration(queue.latency_msec || 0)} latency</div>
			</div>
			<!-- Actions Menu -->
			{#if onPause || onResume || onDelete}
				<div class="relative">
					<button
						type="button"
						class="p-1.5 rounded hover:bg-surface-700 transition-colors"
						onclick={handleMenuClick}
						title="Actions"
					>
						<svg class="w-5 h-5 text-surface-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 5v.01M12 12v.01M12 19v.01M12 6a1 1 0 110-2 1 1 0 010 2zm0 7a1 1 0 110-2 1 1 0 010 2zm0 7a1 1 0 110-2 1 1 0 010 2z" />
						</svg>
					</button>
					{#if menuOpen}
						<!-- svelte-ignore a11y_no_static_element_interactions -->
						<!-- svelte-ignore a11y_click_events_have_key_events -->
						<div
							class="absolute right-0 top-full mt-1 w-36 rounded-lg shadow-lg py-1 z-10 rq-card"
							onclick={(e) => e.stopPropagation()}
							role="menu"
						>
							{#if queue.paused && onResume}
								<button
									type="button"
									class="w-full px-3 py-2 text-left text-sm hover:bg-surface-700 flex items-center gap-2 text-success-500"
									onclick={(e) => handleAction(onResume, e)}
								>
									<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
										<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
										<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
									</svg>
									Resume
								</button>
							{:else if !queue.paused && onPause}
								<button
									type="button"
									class="w-full px-3 py-2 text-left text-sm hover:bg-surface-700 flex items-center gap-2 text-warning-500"
									onclick={(e) => handleAction(onPause, e)}
								>
									<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
										<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z" />
									</svg>
									Pause
								</button>
							{/if}
							{#if onDelete}
								<button
									type="button"
									class="w-full px-3 py-2 text-left text-sm hover:bg-surface-700 flex items-center gap-2 text-error-500"
									onclick={(e) => handleAction(onDelete, e)}
								>
									<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
										<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
									</svg>
									Delete
								</button>
							{/if}
						</div>
					{/if}
				</div>
			{/if}
		</div>
	</div>

	<!-- Workers listening on this queue -->
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

	<div class="grid grid-cols-3 gap-3 text-center">
		<div class="rq-metric-box">
			<div class="text-lg md:text-xl font-bold text-warning-500">{formatNumber(queue.pending || 0)}</div>
			<div class="text-xs text-surface-500 mt-1">Pending</div>
		</div>
		<div class="rq-metric-box">
			<div class="text-lg md:text-xl font-bold text-success-500">{formatNumber(queue.active || 0)}</div>
			<div class="text-xs text-surface-500 mt-1">Active</div>
		</div>
		<div class="rq-metric-box">
			<div class="text-lg md:text-xl font-bold text-tertiary-500">{formatNumber(queue.completed || 0)}</div>
			<div class="text-xs text-surface-500 mt-1">Completed</div>
		</div>
	</div>

	<div class="grid grid-cols-3 gap-3 text-center mt-4">
		<div class="rq-metric-box">
			<div class="text-lg md:text-xl font-bold text-orange-500">{formatNumber(queue.retry || 0)}</div>
			<div class="text-xs text-surface-500 mt-1">Retry</div>
		</div>
		<div class="rq-metric-box">
			<div class="text-lg md:text-xl font-bold text-surface-500">{formatNumber(queue.archived || 0)}</div>
			<div class="text-xs text-surface-500 mt-1">Archived</div>
		</div>
		<div class="rq-metric-box">
			<div class="text-lg md:text-xl font-bold text-error-500">{formatNumber(queue.failed || 0)}</div>
			<div class="text-xs text-surface-500 mt-1">Failed</div>
		</div>
	</div>
</div>
