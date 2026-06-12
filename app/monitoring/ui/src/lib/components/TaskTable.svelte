<script lang="ts">
	import type { Task, TaskState } from '$lib/api/types';
	import { formatRelativeTime, truncateId, getStateBadgeClass } from '$lib/utils/format';
	import JsonViewer from './JsonViewer.svelte';

	interface Props {
		tasks?: Task[];
		taskState: TaskState;
		loading?: boolean;
		selectedIds?: Set<string>;
		onselect?: (detail: { id: string; selected: boolean }) => void;
		onselectall?: (detail: { selected: boolean }) => void;
		onaction?: (detail: { action: string; taskId: string }) => void;
	}

	let {
		tasks = [],
		taskState,
		loading = false,
		selectedIds = new Set(),
		onselect,
		onselectall,
		onaction
	}: Props = $props();

	let expandedId = $state<string | null>(null);
	let searchQuery = $state('');

	let filteredTasks = $derived(tasks.filter((task) => {
		if (!searchQuery) return true;
		const query = searchQuery.toLowerCase();
		return (
			(task.id || '').toLowerCase().includes(query) ||
			(task.type || '').toLowerCase().includes(query) ||
			(task.payload || '').toLowerCase().includes(query)
		);
	}));

	let allSelected = $derived(filteredTasks.length > 0 && filteredTasks.every((t) => selectedIds.has(t.id)));
	let someSelected = $derived(filteredTasks.some((t) => selectedIds.has(t.id)));

	// Determine which actions are available based on taskState
	let canCancel = $derived(taskState === 'active');
	let canRun = $derived(taskState === 'scheduled' || taskState === 'retry' || taskState === 'archived');
	let canArchive = $derived(taskState === 'pending' || taskState === 'scheduled' || taskState === 'retry');
	let canDelete = $derived(taskState !== 'active');

	function toggleExpand(taskId: string) {
		expandedId = expandedId === taskId ? null : taskId;
	}

	function handleSelectAll(e: Event) {
		const checked = (e.target as HTMLInputElement).checked;
		onselectall?.({ selected: checked });
	}

	function handleSelect(taskId: string, e: Event) {
		const checked = (e.target as HTMLInputElement).checked;
		onselect?.({ id: taskId, selected: checked });
	}

	function handleAction(action: string, taskId: string) {
		onaction?.({ action, taskId });
	}

	async function copyTaskId(taskId: string) {
		try {
			await navigator.clipboard.writeText(taskId);
		} catch (err) {
			console.error('Failed to copy:', err);
		}
	}
</script>

<div class="space-y-3">
	<!-- Search -->
	<div class="flex items-center gap-3">
		<div class="relative flex-1">
			<svg
				class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-surface-500"
				fill="none"
				stroke="currentColor"
				viewBox="0 0 24 24"
			>
				<path
					stroke-linecap="round"
					stroke-linejoin="round"
					stroke-width="2"
					d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
				/>
			</svg>
			<input
				type="text"
				bind:value={searchQuery}
				placeholder="Search by ID, type, or payload..."
				class="input pl-10 pr-4 py-2 w-full"
			/>
		</div>
		<span class="text-sm text-surface-500">
			{filteredTasks.length} task{filteredTasks.length !== 1 ? 's' : ''}
		</span>
	</div>

	<!-- Table -->
	<div class="table-container">
		<table class="rq-table">
			<thead>
				<tr>
					<th class="w-10">
						<input
							type="checkbox"
							class="checkbox"
							checked={allSelected}
							indeterminate={someSelected && !allSelected}
							onchange={handleSelectAll}
						/>
					</th>
					<th>Task ID</th>
					<th>Type</th>
					<th>Queue</th>
					{#if taskState === 'retry' || taskState === 'archived'}
						<th>Retries</th>
					{/if}
					{#if taskState === 'scheduled' || taskState === 'retry'}
						<th>Next Run</th>
					{/if}
					{#if taskState === 'completed'}
						<th>Completed</th>
					{/if}
					{#if taskState === 'archived'}
						<th>Last Error</th>
					{/if}
					<th class="w-24">Actions</th>
				</tr>
			</thead>
			<tbody>
				{#if loading}
					{#each Array(5) as _}
						<tr>
							<td colspan="7">
								<div class="animate-pulse h-4 bg-surface-300 dark:bg-surface-600 rounded"></div>
							</td>
						</tr>
					{/each}
				{:else if filteredTasks.length === 0}
					<tr>
						<td colspan="7" class="text-center py-8 text-surface-500">
							{searchQuery ? 'No tasks match your search' : 'No tasks in this state'}
						</td>
					</tr>
				{:else}
					{#each filteredTasks as task, i (task.id || `_idx_${i}`)}
						<tr class="task-row">
							<td>
								<input
									type="checkbox"
									class="checkbox"
									checked={selectedIds.has(task.id)}
									onchange={(e) => handleSelect(task.id, e)}
								/>
							</td>
							<td>
								<div class="flex items-center gap-2">
									<button
										type="button"
										class="font-mono text-sm text-primary-500 hover:text-primary-700 dark:hover:text-primary-300"
										onclick={() => toggleExpand(task.id)}
										title={task.id}
									>
										{truncateId(task.id, 16)}
									</button>
									<button
										type="button"
										class="p-1 hover:bg-surface-200 dark:hover:bg-surface-700 rounded"
										onclick={() => copyTaskId(task.id)}
										title="Copy ID"
									>
										<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path
												stroke-linecap="round"
												stroke-linejoin="round"
												stroke-width="2"
												d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
											/>
										</svg>
									</button>
								</div>
							</td>
							<td>
								<span class="badge preset-outlined-secondary-500 text-xs">{task.type}</span>
							</td>
							<td class="text-sm text-surface-600 dark:text-surface-400">{task.queue}</td>
							{#if taskState === 'retry' || taskState === 'archived'}
								<td class="text-sm">
									{task.retried}/{task.max_retry}
								</td>
							{/if}
							{#if taskState === 'scheduled' || taskState === 'retry'}
								<td class="text-sm text-surface-500">
									{formatRelativeTime(task.next_process_at)}
								</td>
							{/if}
							{#if taskState === 'completed'}
								<td class="text-sm text-surface-500">
									{task.completed_at ? formatRelativeTime(task.completed_at) : '-'}
								</td>
							{/if}
							{#if taskState === 'archived'}
								<td class="text-sm text-error-500 max-w-[200px] truncate" title={task.error_message}>
									{task.error_message || '-'}
								</td>
							{/if}
							<td>
								<div class="flex items-center gap-1">
									{#if canCancel}
										<button
											type="button"
											class="btn btn-sm preset-outlined-warning-500"
											onclick={() => handleAction('cancel', task.id)}
											title="Cancel"
										>
											<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
												<path
													stroke-linecap="round"
													stroke-linejoin="round"
													stroke-width="2"
													d="M6 18L18 6M6 6l12 12"
												/>
											</svg>
										</button>
									{/if}
									{#if canRun}
										<button
											type="button"
											class="btn btn-sm preset-outlined-success-500"
											onclick={() => handleAction('run', task.id)}
											title="Run now"
										>
											<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
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
										</button>
									{/if}
									{#if canArchive}
										<button
											type="button"
											class="btn btn-sm preset-outlined-surface-500"
											onclick={() => handleAction('archive', task.id)}
											title="Archive"
										>
											<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
												<path
													stroke-linecap="round"
													stroke-linejoin="round"
													stroke-width="2"
													d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4"
												/>
											</svg>
										</button>
									{/if}
									{#if canDelete}
										<button
											type="button"
											class="btn btn-sm preset-outlined-error-500"
											onclick={() => handleAction('delete', task.id)}
											title="Delete"
										>
											<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
												<path
													stroke-linecap="round"
													stroke-linejoin="round"
													stroke-width="2"
													d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
												/>
											</svg>
										</button>
									{/if}
								</div>
							</td>
						</tr>
						{#if expandedId === task.id}
							<tr class="bg-surface-100 dark:bg-surface-800">
								<td colspan="7" class="p-4">
									<!-- The table can scroll horizontally (min-width); keep the expanded
									     detail pinned to the visible viewport so it reads without scrolling. -->
									<div class="sticky left-0 max-w-[calc(100vw-3rem)] md:max-w-full space-y-4">
									<div class="space-y-4">
										<div>
											<h4 class="text-sm font-semibold mb-2">Payload</h4>
											<JsonViewer data={task.payload} collapsed={false} maxHeight="240px" />
										</div>
										{#if task.result}
											<div>
												<h4 class="text-sm font-semibold mb-2">Result</h4>
												<JsonViewer data={task.result} collapsed={false} maxHeight="240px" />
											</div>
										{/if}
									</div>
									{#if task.error_message}
										<div class="mt-4">
											<h4 class="text-sm font-semibold mb-2 text-error-500">Last Error</h4>
											<pre
												class="p-3 rounded bg-error-100 dark:bg-error-900/30 text-error-700 dark:text-error-300 text-sm whitespace-pre-wrap">{task.error_message}</pre>
										</div>
									{/if}
									<div class="mt-4 grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
										<div>
											<span class="text-surface-500">Timeout:</span>
											<span class="font-mono ml-2">{task.timeout_seconds}s</span>
										</div>
										<div>
											<span class="text-surface-500">Max Retry:</span>
											<span class="font-mono ml-2">{task.max_retry}</span>
										</div>
										<div>
											<span class="text-surface-500">Retried:</span>
											<span class="font-mono ml-2">{task.retried}</span>
										</div>
										{#if task.group}
											<div>
												<span class="text-surface-500">Group:</span>
												<span class="font-mono ml-2">{task.group}</span>
											</div>
										{/if}
									</div>
									</div>
								</td>
							</tr>
						{/if}
					{/each}
				{/if}
			</tbody>
		</table>
	</div>
</div>
