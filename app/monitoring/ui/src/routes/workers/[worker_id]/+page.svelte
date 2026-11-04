<script lang="ts">
	import { parseWorkerQueues } from '$lib/utils/workerQueues';
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/stores';
	import { base } from '$app/paths';
	import { ArrowLeft, Cpu, Download, GitBranch, Pause, Play, RotateCw } from '@lucide/svelte';
	import { workersStore } from '$lib/stores/workers';
	import { settings } from '$lib/stores/settings';
	import { getWorkerLogs, sendControl } from '$lib/api/client';
	import { formatBytes, formatRelativeTime } from '$lib/utils/format';
	import { workerState, workerStatus } from '$lib/utils/status';
	import { Badge, Button, EmptyState, KeyValue, PageHeader, Panel, ProgressBar, Shimmer } from '$lib/components/ui';
	import { toaster } from '$lib/stores/toaster';
	import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
	import type { Worker, LogLine, ControlAction } from '$lib/api/types';

	let workerId = $derived(decodeURIComponent($page.params.worker_id ?? ''));

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

	// Control channel (drain, resume, code/worker update, config reload)
	let canControl = $derived((worker?.control_actions?.length ?? 0) > 0);
	let held = $derived(worker?.held ?? []);
	let lastCommand = $derived(worker?.last_command);
	let updateVersion = $state('');
	let sending = $state(false);
	let confirmDialog = $state({ open: false, title: '', message: '', confirmText: 'Confirm', action: () => {} });

	const actionLabels: Record<ControlAction, string> = {
		drain: 'Drain',
		resume: 'Resume',
		code_update: 'Update code',
		worker_update: 'Update worker',
		config_reload: 'Reload config'
	};

	async function control(action: ControlAction, version?: string) {
		sending = true;
		try {
			const res = await sendControl({ action, target: workerId, version });
			if (res.receivers === 0) {
				toaster.error({ title: 'Not delivered', description: 'The worker is not listening on its control channel.' });
			} else {
				toaster.success({ title: `${actionLabels[action]} sent`, description: 'The result appears below after the next heartbeat.' });
			}
			workersStore.fetch();
		} catch (e) {
			toaster.error({ title: 'Error', description: e instanceof Error ? e.message : 'Failed to send command' });
		} finally {
			sending = false;
		}
	}

	function confirmControl(action: ControlAction, message: string, version?: string) {
		confirmDialog = {
			open: true,
			title: actionLabels[action],
			message,
			confirmText: actionLabels[action],
			action: () => control(action, version)
		};
	}

	let status = $derived(worker ? workerState(worker, $workersStore.servers) : null);

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

<div class="rq-page space-y-4 md:space-y-6">
	<PageHeader
		title={workerId}
		mono
		breadcrumbs={[{ label: 'Workers', href: '/workers' }, { label: workerId }]}
		onrefresh={() => workersStore.fetch()}
	>
		{#snippet titleExtra()}
			{#if status}<Badge status={workerStatus(status)} />{/if}
		{/snippet}
	</PageHeader>

	{#if worker}
		<Panel title="Overview">
			<div class="space-y-5">
				<KeyValue
					columns={3}
					items={[
						{ label: 'Version', value: worker.version || '—', mono: true },
						{ label: 'Concurrency', value: worker.concurrency },
						{ label: 'Started', value: formatRelativeTime(new Date(worker.started_at * 1000)) },
						{ label: 'Last heartbeat', value: formatRelativeTime(new Date(worker.last_beat * 1000)) }
					]}
				/>
				<div>
					<h3 class="text-xs font-medium text-fg-subtle mb-1.5">Queues</h3>
					<div class="flex flex-wrap gap-1">
						{#each parseWorkerQueues(worker.queues) as queue (queue.name)}
							<Badge tone="outline">
								<span>{queue.name}</span>
								<span class="text-fg-faint ml-1 font-mono tabular-nums">p:{queue.priority}</span>
							</Badge>
						{/each}
					</div>
				</div>
			</div>
		</Panel>

		{#if m}
			<Panel title="Resources">
				<!-- Figures sit directly in the panel (no box-in-a-box). -->
				<div class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-x-8 gap-y-5">
					<div class="min-w-0 space-y-2">
						<div>
							<div class="text-xs text-fg-subtle">CPU</div>
							<div class="mt-0.5 text-lg font-semibold tracking-tight tabular-nums text-fg">{m.cpu_percent.toFixed(1)}%</div>
						</div>
						<ProgressBar value={m.cpu_percent} />
					</div>
					<div class="min-w-0 space-y-2">
						<div>
							<div class="text-xs text-fg-subtle">Memory</div>
							<div class="mt-0.5 text-lg font-semibold tracking-tight tabular-nums text-fg">
								{formatBytes(m.memory_used_bytes)}
								<span class="text-ui font-normal text-fg-subtle">/ {formatBytes(m.memory_total_bytes)}</span>
							</div>
						</div>
						<ProgressBar value={memPercent} />
					</div>
					{#if m.gpus && m.gpus.length > 0}
						{#each m.gpus as gpu (gpu.index)}
							{@const gpuMemPercent = gpu.memory_total_mb > 0 ? (gpu.memory_used_mb / gpu.memory_total_mb) * 100 : 0}
							<div class="min-w-0 space-y-2">
								<div class="min-w-0">
									<div class="text-xs text-fg-subtle truncate" title={gpu.name}>GPU {gpu.index} · {gpu.name}</div>
									<div class="mt-0.5 text-lg font-semibold tracking-tight tabular-nums text-fg">
										{gpu.utilization_percent.toFixed(0)}%
									</div>
								</div>
								<ProgressBar value={gpu.utilization_percent} />
								<div class="flex justify-between gap-2 text-xs text-fg-subtle tabular-nums">
									<span>VRAM {gpu.memory_used_mb}/{gpu.memory_total_mb} MB ({gpuMemPercent.toFixed(0)}%)</span>
									<span>{gpu.temperature_c}°C</span>
								</div>
							</div>
						{/each}
					{/if}
				</div>
			</Panel>
		{/if}

		<Panel title="Control">
			{#snippet titleExtra()}
				{#if held.length > 0}
					<Badge tone="warning">Not taking tasks: {held.join(', ')}</Badge>
				{/if}
			{/snippet}

			{#if !canControl}
				<p class="text-ui text-fg-muted">
					This worker predates the control channel. Upgrade it to runqy-worker v0.4.0 or later to drain or update it
					from here.
				</p>
			{:else}
				<div class="space-y-4">
					<div class="flex flex-wrap gap-2">
						{#if held.length > 0}
							<Button icon={Play} disabled={sending} onclick={() => control('resume')}>Resume</Button>
						{:else}
							<Button
								icon={Pause}
								disabled={sending}
								onclick={() =>
									confirmControl('drain', 'The worker stops taking new tasks; tasks in flight finish. Use Resume to undo.')}
								>Drain</Button
							>
						{/if}
						<Button
							icon={GitBranch}
							disabled={sending}
							onclick={() =>
								confirmControl(
									'code_update',
									'The worker waits for its tasks to finish, updates the code to the latest commit of the queue branch, reinstalls dependencies and restarts the task process (models reload).'
								)}>Update code</Button
						>
						<Button icon={RotateCw} disabled={sending} onclick={() => control('config_reload')}>Reload config</Button>
					</div>

					<div class="flex flex-wrap items-center gap-2">
						<input
							class="input w-44"
							aria-label="Worker version"
							placeholder="Version, e.g. 0.4.1"
							bind:value={updateVersion}
						/>
						<Button
							icon={Download}
							disabled={sending || !updateVersion.trim()}
							onclick={() =>
								confirmControl(
									'worker_update',
									`The worker downloads runqy-worker ${updateVersion.trim()}, waits for its tasks to finish and restarts into it in the same container.`,
									updateVersion.trim()
								)}>Update worker</Button
						>
					</div>

					{#if lastCommand}
						<div class="pt-4 border-t border-line-subtle text-ui">
							<div class="flex flex-wrap items-center gap-2">
								<span class="text-fg-subtle">Last command</span>
								<span class="font-mono text-fg">{lastCommand.action}</span>
								{#if lastCommand.state === 'done'}
									<Badge tone="success">done</Badge>
								{:else if lastCommand.state === 'failed'}
									<Badge tone="danger">failed</Badge>
								{:else}
									<Badge tone="accent">running</Badge>
								{/if}
								<span class="text-fg-subtle">{formatRelativeTime(new Date(lastCommand.at * 1000))}</span>
							</div>
							{#if lastCommand.message}
								<div class="text-fg-muted mt-1 break-all">{lastCommand.message}</div>
							{/if}
						</div>
					{/if}
				</div>
			{/if}
		</Panel>

		<Panel title="Logs" flush>
			{#snippet actions()}
				<label class="flex items-center gap-2 text-ui text-fg-muted cursor-pointer">
					<input type="checkbox" bind:checked={pinToBottom} class="checkbox" />
					Auto-scroll
				</label>
			{/snippet}
			<div
				bind:this={logContainer}
				class="min-h-[120px] max-h-[60vh] overflow-y-auto bg-canvas rounded-b-lg px-4 py-3 font-mono text-xs text-fg"
			>
				{#if logsLoading}
					<Shimmer lines={4} />
				{:else if logLines.length === 0}
					<p class="font-sans text-ui text-fg-subtle">
						No logs yet. They appear when the task process writes to stderr or non-JSON stdout.
					</p>
				{:else}
					{#each logLines as line (line.seq || line.ts)}
						<div class="flex gap-2 py-px hover:bg-hover">
							<span class="text-fg-faint shrink-0 tabular-nums">{formatLogTime(line.ts)}</span>
							<span class="{line.src === 'stderr' ? 'text-error-300' : 'text-fg-subtle'} shrink-0 w-14">[{line.src}]</span>
							<span class="break-all">{line.text}</span>
						</div>
					{/each}
				{/if}
			</div>
		</Panel>
	{:else}
		<div class="rq-card">
			<EmptyState icon={Cpu} title="Worker not found" description="Worker not found or no longer connected.">
				{#snippet action()}
					<Button href="{base}/workers" icon={ArrowLeft}>Back to Workers</Button>
				{/snippet}
			</EmptyState>
		</div>
	{/if}
</div>

<ConfirmDialog
	bind:open={confirmDialog.open}
	title={confirmDialog.title}
	message={confirmDialog.message}
	confirmText={confirmDialog.confirmText}
	onconfirm={confirmDialog.action}
/>
