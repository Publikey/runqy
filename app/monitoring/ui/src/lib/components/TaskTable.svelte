<script lang="ts">
	import type { Task, TaskState } from '$lib/api/types';
	import { formatRelativeTime, truncateId } from '$lib/utils/format';
	import { Search, Copy, X, Play, Archive, Trash2, Inbox } from '@lucide/svelte';
	import { Badge, IconButton, EmptyState, Shimmer, KeyValue } from '$lib/components/ui';
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

	// Columns: checkbox, id, type, queue, state-specific ones, actions.
	let colCount = $derived(
		5 +
			(taskState === 'retry' || taskState === 'archived' ? 1 : 0) +
			(taskState === 'scheduled' || taskState === 'retry' ? 1 : 0) +
			(taskState === 'completed' ? 1 : 0) +
			(taskState === 'archived' ? 1 : 0)
	);

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

<div>
	<!-- Search -->
	<div class="flex items-center gap-3 px-4 md:px-5 py-3 border-b border-line-subtle">
		<div class="relative flex-1 min-w-0">
			<Search
				size={16}
				strokeWidth={1.75}
				class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-fg-subtle"
			/>
			<input
				type="text"
				bind:value={searchQuery}
				placeholder="Search by ID, type, or payload..."
				aria-label="Search tasks"
				class="input pl-9 w-full"
			/>
		</div>
		<span class="shrink-0 text-ui text-fg-subtle tabular-nums">
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
							aria-label="Select all tasks"
							checked={allSelected}
							indeterminate={someSelected && !allSelected}
							onchange={handleSelectAll}
						/>
					</th>
					<th>Task ID</th>
					<th>Type</th>
					<th>Queue</th>
					{#if taskState === 'retry' || taskState === 'archived'}
						<th class="text-right">Retries</th>
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
					<th class="w-24 text-right"><span class="sr-only">Actions</span></th>
				</tr>
			</thead>
			<tbody>
				{#if loading}
					{#each Array(5) as _, i (i)}
						<tr>
							<td colspan={colCount}>
								<Shimmer class="h-4 w-full" />
							</td>
						</tr>
					{/each}
				{:else if filteredTasks.length === 0}
					<tr>
						<td colspan={colCount} class="p-0">
							<EmptyState
								icon={Inbox}
								title={searchQuery ? 'No tasks match your search' : 'No tasks in this state'}
								compact
							/>
						</td>
					</tr>
				{:else}
					{#each filteredTasks as task, i (task.id || `_idx_${i}`)}
						<tr class="task-row">
							<td>
								<input
									type="checkbox"
									class="checkbox"
									aria-label="Select task {task.id}"
									checked={selectedIds.has(task.id)}
									onchange={(e) => handleSelect(task.id, e)}
								/>
							</td>
							<td>
								<div class="flex items-center gap-1">
									<button
										type="button"
										class="font-mono text-ui text-primary-400 hover:text-primary-300 transition-colors"
										onclick={() => toggleExpand(task.id)}
										title={task.id}
										aria-expanded={expandedId === task.id}
									>
										{truncateId(task.id, 16)}
									</button>
									<IconButton icon={Copy} label="Copy ID" onclick={() => copyTaskId(task.id)} />
								</div>
							</td>
							<td>
								<Badge tone="outline" class="font-mono">{task.type}</Badge>
							</td>
							<td class="font-mono text-fg-muted">{task.queue}</td>
							{#if taskState === 'retry' || taskState === 'archived'}
								<td class="text-right tabular-nums text-fg-muted">
									{task.retried}/{task.max_retry}
								</td>
							{/if}
							{#if taskState === 'scheduled' || taskState === 'retry'}
								<td class="text-fg-muted">
									{formatRelativeTime(task.next_process_at)}
								</td>
							{/if}
							{#if taskState === 'completed'}
								<td class="text-fg-muted">
									{task.completed_at ? formatRelativeTime(task.completed_at) : '-'}
								</td>
							{/if}
							{#if taskState === 'archived'}
								<td class="text-error-300 max-w-[200px] truncate" title={task.error_message}>
									{task.error_message || '-'}
								</td>
							{/if}
							<td>
								<div class="flex items-center justify-end gap-1">
									{#if canCancel}
										<IconButton icon={X} label="Cancel" onclick={() => handleAction('cancel', task.id)} />
									{/if}
									{#if canRun}
										<IconButton
											icon={Play}
											label="Run now"
											variant="secondary"
											onclick={() => handleAction('run', task.id)}
										/>
									{/if}
									{#if canArchive}
										<IconButton
											icon={Archive}
											label="Archive"
											danger
											onclick={() => handleAction('archive', task.id)}
										/>
									{/if}
									{#if canDelete}
										<IconButton
											icon={Trash2}
											label="Delete"
											danger
											onclick={() => handleAction('delete', task.id)}
										/>
									{/if}
								</div>
							</td>
						</tr>
						{#if expandedId === task.id}
							<tr class="bg-raised">
								<td colspan={colCount} class="p-4">
									<!-- The table can scroll horizontally (min-width); keep the expanded
									     detail pinned to the visible viewport so it reads without scrolling. -->
									<div class="sticky left-0 max-w-[calc(100vw-3rem)] md:max-w-full space-y-4">
										<div>
											<h3 class="text-xs font-medium text-fg-subtle mb-2">Payload</h3>
											<JsonViewer data={task.payload} collapsed={false} maxHeight="240px" />
										</div>
										{#if task.result}
											<div>
												<h3 class="text-xs font-medium text-fg-subtle mb-2">Result</h3>
												<JsonViewer data={task.result} collapsed={false} maxHeight="240px" />
											</div>
										{/if}
										{#if task.error_message}
											<div>
												<h3 class="text-xs font-medium text-fg-subtle mb-2">Last Error</h3>
												<pre
													class="m-0 rounded-md border border-error-500/30 bg-error-500/10 text-error-200 text-ui p-3 whitespace-pre-wrap break-words">{task.error_message}</pre>
											</div>
										{/if}
										<KeyValue
											columns={3}
											items={[
												{ label: 'Timeout', value: `${task.timeout_seconds}s`, mono: true },
												{ label: 'Max Retry', value: task.max_retry, mono: true },
												{ label: 'Retried', value: task.retried, mono: true },
												...(task.group ? [{ label: 'Group', value: task.group, mono: true }] : [])
											]}
										/>
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
