<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { base } from '$app/paths';
	import { SegmentedControl } from '@skeletonlabs/skeleton-svelte';
	import { autoscaleStore } from '$lib/stores/autoscale';
	import { settings } from '$lib/stores/settings';
	import { toaster } from '$lib/stores/toaster';
	import {
		protectInstance,
		unprotectInstance,
		getAutoscaleProviderTypes,
		createAutoscaleProvider,
		updateAutoscaleProvider,
		deleteAutoscaleProvider,
		getQueueConfigs,
		getQueueConfig,
		updateQueueConfig,
		getAutoscaleInstanceLogs
	} from '$lib/api/client';
	import type {
		AutoscaleConfig,
		AutoscaleInstance,
		AutoscaleInstanceLogs,
		AutoscalePause,
		AutoscaleProvider,
		DeploymentConfig,
		QueueConfigDetail,
		ScaleTrigger
	} from '$lib/api/types';
	import { formatRelativeTime, formatDateTime } from '$lib/utils/format';
	import { parseQueueName } from '$lib/utils/queueGrouping';
	import ProviderModal from '$lib/components/ProviderModal.svelte';
	import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
	import QueueConfigModal from '$lib/components/QueueConfigModal.svelte';

	let pollInterval: ReturnType<typeof setInterval> | null = null;
	let refreshing = $state(false);

	// ---- Instances: filter + details ----

	type InstanceFilter = 'active' | 'failed' | 'terminated' | 'all';
	const FILTER_KEY = 'runqy.autoscaling.instanceFilter';
	const ACTIVE_STATUSES = ['provisioning', 'running', 'draining', 'destroying'];

	function loadFilter(): InstanceFilter {
		try {
			const v = localStorage.getItem(FILTER_KEY);
			if (v === 'active' || v === 'failed' || v === 'terminated' || v === 'all') return v;
		} catch {
			// storage unavailable: use the default
		}
		return 'active';
	}

	let instanceFilter = $state<InstanceFilter>(loadFilter());
	let expanded = $state<Set<string>>(new Set());

	function setFilter(v: InstanceFilter) {
		instanceFilter = v;
		try {
			localStorage.setItem(FILTER_KEY, v);
		} catch {
			// ignore
		}
	}

	function matchesFilter(i: AutoscaleInstance, f: InstanceFilter): boolean {
		switch (f) {
			case 'active':
				return ACTIVE_STATUSES.includes(i.status);
			case 'failed':
				return i.status === 'failed';
			case 'terminated':
				return i.status === 'terminated';
			default:
				return true;
		}
	}

	let counts = $derived({
		active: $autoscaleStore.instances.filter((i) => matchesFilter(i, 'active')).length,
		failed: $autoscaleStore.instances.filter((i) => matchesFilter(i, 'failed')).length,
		terminated: $autoscaleStore.instances.filter((i) => matchesFilter(i, 'terminated')).length,
		all: $autoscaleStore.instances.length
	});

	let visibleInstances = $derived(
		$autoscaleStore.instances.filter((i) => matchesFilter(i, instanceFilter))
	);

	function toggleExpanded(id: string) {
		const next = new Set(expanded);
		if (next.has(id)) next.delete(id);
		else next.add(id);
		expanded = next;
	}

	function instanceReason(i: AutoscaleInstance): string {
		return i.error ? `Provisioning failed: ${i.error}` : i.reason || '';
	}

	// Machine logs, loaded on demand per instance: live from the provider while the
	// machine exists, otherwise those saved when the autoscaler destroyed it.
	interface LogsState {
		loading: boolean;
		data?: AutoscaleInstanceLogs;
		error?: string;
	}
	let logsById = $state<Record<string, LogsState>>({});

	function isEnded(i: AutoscaleInstance): boolean {
		return i.status === 'failed' || i.status === 'terminated';
	}

	async function loadLogs(i: AutoscaleInstance) {
		logsById[i.instance_id] = { loading: true };
		try {
			logsById[i.instance_id] = { loading: false, data: await getAutoscaleInstanceLogs(i.instance_id) };
		} catch (e) {
			logsById[i.instance_id] = {
				loading: false,
				error: e instanceof Error ? e.message : 'Failed to fetch logs'
			};
		}
	}

	// ---- Provisioning pauses ----

	function pausesFor(parent: string): AutoscalePause[] {
		return $autoscaleStore.pauses.filter((p) => p.queue === parent);
	}

	function formatUntil(until: string): string {
		const d = new Date(until);
		const mins = Math.max(0, Math.round((d.getTime() - Date.now()) / 60000));
		const time = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
		return mins >= 1 ? `${time} (in ${mins} min)` : `${time} (< 1 min)`;
	}

	// ---- Queue autoscale configs ----

	interface QueueAutoscaleRow {
		parent: string;
		configName: string; // a full queue name, used to load/edit the shared deployment
		autoscale: AutoscaleConfig;
	}

	let queueRows = $state<QueueAutoscaleRow[]>([]);
	let queueConfigModalOpen = $state(false);
	let queueConfigModalLoading = $state(false);
	let selectedQueueConfig = $state<QueueConfigDetail | null>(null);

	async function loadQueueConfigs() {
		try {
			const response = await getQueueConfigs();
			const byParent = new Map<string, QueueAutoscaleRow>();
			for (const q of response.queues) {
				const asc = q.deployment?.autoscale;
				if (!asc) continue;
				const { parent } = parseQueueName(q.name);
				if (!byParent.has(parent)) {
					byParent.set(parent, { parent, configName: q.name, autoscale: asc });
				}
			}
			queueRows = [...byParent.values()].sort((a, b) => a.parent.localeCompare(b.parent));
		} catch (e) {
			console.error('Failed to load queue configs:', e);
		}
	}

	function providerFor(name: string): AutoscaleProvider | undefined {
		return $autoscaleStore.providers.find((p) => p.name === name);
	}

	function liveCount(parent: string): number {
		return $autoscaleStore.instances.filter(
			(i) => i.queue === parent && ACTIVE_STATUSES.includes(i.status)
		).length;
	}

	function subQueuesSuffix(t: ScaleTrigger): string {
		return t.sub_queues?.length ? ` [${t.sub_queues.join(', ')}]` : '';
	}

	function describeTrigger(t: ScaleTrigger, direction: 'up' | 'down'): string {
		switch (t.trigger) {
			case 'no_workers':
				return 'no workers' + subQueuesSuffix(t);
			case 'queue_depth':
				return `pending ≥ ${t.threshold ?? '?'}` + subQueuesSuffix(t);
			case 'latency':
				return `latency > ${t.max_latency ?? '?'}` + subQueuesSuffix(t);
			case 'idle':
				return `idle ${t.timeout ?? '?'}`;
			case 'schedule':
				if (direction === 'up') {
					return `cron "${t.cron}" → ${t.workers ?? '?'}${t.duration ? ` for ${t.duration}` : ''}`;
				}
				return `cron "${t.cron}" → min`;
			default:
				return t.trigger;
		}
	}

	function describeInstance(asc: AutoscaleConfig): string[] {
		const out: string[] = [];
		const spec = asc.instance || {};
		if (spec.gpu) out.push(spec.gpu);
		if (spec.disk_gb) out.push(`${spec.disk_gb} GB disk`);
		if (spec.min_ram_gb) out.push(`≥ ${spec.min_ram_gb} GB RAM`);
		if (spec.min_vcpu) out.push(`≥ ${spec.min_vcpu} vCPU`);
		if (spec.max_price_per_hour) out.push(`≤ $${spec.max_price_per_hour}/hr`);
		if (spec.image) out.push(spec.image);
		return out;
	}

	async function openEditQueueConfig(name: string) {
		try {
			selectedQueueConfig = await getQueueConfig(name);
			queueConfigModalOpen = true;
		} catch (e) {
			toaster.error({
				title: 'Error',
				description: `Failed to load queue config: ${e instanceof Error ? e.message : 'Unknown error'}`
			});
		}
	}

	async function handleSaveQueueConfig(
		queues: { name: string; priority: number; deployment: DeploymentConfig | null }[]
	) {
		const q = queues[0];
		if (!q) return;
		queueConfigModalLoading = true;
		try {
			await updateQueueConfig(q.name, q.priority, q.deployment || undefined);
			toaster.success({ title: 'Queue Updated', description: `Queue "${q.name}" updated` });
			await loadData();
			queueConfigModalOpen = false;
		} catch (e) {
			toaster.error({
				title: 'Error',
				description: e instanceof Error ? e.message : 'Failed to save queue config'
			});
		} finally {
			queueConfigModalLoading = false;
		}
	}

	let providerTypes = $state<string[]>([]);
	let providerModalOpen = $state(false);
	let providerModalMode = $state<'create' | 'edit'>('create');
	let providerModalLoading = $state(false);
	let selectedProvider = $state<AutoscaleProvider | null>(null);

	let confirmDialog = $state({
		open: false,
		title: '',
		message: '',
		action: () => {}
	});

	// Protection only matters for machines the autoscaler could still destroy.
	function isLiveInstance(i: AutoscaleInstance): boolean {
		return i.status === 'provisioning' || i.status === 'running';
	}

	// A "running" machine without a worker yet is still booting (image pull, dependencies).
	function displayStatus(i: AutoscaleInstance): string {
		return i.status === 'running' && !i.worker_id ? 'booting' : i.status;
	}

	function statusBadgeClass(status: string): string {
		switch (status) {
			case 'running':
				return 'badge preset-filled-success-500 text-xs';
			case 'provisioning':
			case 'booting':
				return 'badge preset-filled-secondary-500 text-xs';
			case 'draining':
				return 'badge preset-filled-warning-500 text-xs';
			case 'destroying':
				return 'badge preset-filled-warning-600 text-xs';
			case 'failed':
				return 'badge preset-filled-error-500 text-xs';
			case 'terminated':
				return 'badge preset-filled-surface-500 text-xs';
			default:
				return 'badge preset-filled-surface-500 text-xs';
		}
	}

	function formatCost(value: number): string {
		return `$${(value ?? 0).toFixed(2)}`;
	}

	async function loadData() {
		await Promise.all([autoscaleStore.fetch(), loadQueueConfigs()]);
	}

	async function handleRefresh() {
		refreshing = true;
		await loadData();
		setTimeout(() => { refreshing = false; }, 500);
	}

	onMount(async () => {
		loadData();
		try {
			const response = await getAutoscaleProviderTypes();
			providerTypes = response.types || [];
		} catch {
			providerTypes = [];
		}
		pollInterval = setInterval(loadData, $settings.pollInterval * 1000);
	});

	onDestroy(() => {
		if (pollInterval) clearInterval(pollInterval);
	});

	async function handleToggleProtect(instance: AutoscaleInstance) {
		try {
			if (instance.protected) {
				await unprotectInstance(instance.instance_id);
				toaster.success({ title: 'Instance Unprotected', description: instance.instance_id });
			} else {
				await protectInstance(instance.instance_id);
				toaster.success({ title: 'Instance Protected', description: instance.instance_id });
			}
			await loadData();
		} catch (e) {
			const errorMessage = e instanceof Error ? e.message : 'Failed to update protection';
			toaster.error({ title: 'Error', description: errorMessage });
		}
	}

	function openCreateProvider() {
		selectedProvider = null;
		providerModalMode = 'create';
		providerModalOpen = true;
	}

	function openEditProvider(provider: AutoscaleProvider) {
		selectedProvider = provider;
		providerModalMode = 'edit';
		providerModalOpen = true;
	}

	async function handleSaveProvider(body: {
		name: string;
		provider_type: string;
		config: Record<string, unknown>;
		enabled: boolean;
	}) {
		providerModalLoading = true;
		try {
			if (providerModalMode === 'create') {
				await createAutoscaleProvider(body);
				toaster.success({ title: 'Provider Created', description: `Provider "${body.name}" created` });
			} else {
				await updateAutoscaleProvider(body.name, {
					provider_type: body.provider_type,
					config: body.config,
					enabled: body.enabled
				});
				toaster.success({ title: 'Provider Updated', description: `Provider "${body.name}" updated` });
			}
			await loadData();
			providerModalOpen = false;
		} catch (e) {
			const errorMessage = e instanceof Error ? e.message : 'Failed to save provider';
			toaster.error({ title: 'Error', description: errorMessage });
		} finally {
			providerModalLoading = false;
		}
	}

	function confirmDeleteProvider(name: string) {
		confirmDialog = {
			open: true,
			title: 'Delete Provider',
			message: `Are you sure you want to delete provider "${name}"? This action cannot be undone.`,
			action: async () => {
				try {
					await deleteAutoscaleProvider(name);
					await loadData();
					toaster.success({ title: 'Provider Deleted', description: `Provider "${name}" deleted` });
				} catch (e) {
					const errorMessage = e instanceof Error ? e.message : 'Failed to delete provider';
					toaster.error({ title: 'Error', description: errorMessage });
				}
			}
		};
	}
</script>

<svelte:head>
	<title>Autoscaling - runqy Monitor</title>
</svelte:head>

<div class="p-4 md:p-6 space-y-8">
	<!-- Header -->
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<h1 class="text-2xl font-bold">Autoscaling</h1>
			<p class="text-surface-500">
				{counts.active} active instance{counts.active !== 1 ? 's' : ''}
				{#if $autoscaleStore.lastUpdated}
					&middot; Updated {formatRelativeTime($autoscaleStore.lastUpdated)}
				{/if}
			</p>
		</div>
		<div class="flex items-center gap-2">
			<button
				type="button"
				class="rq-btn-ghost {refreshing ? 'refresh-spinning' : ''}"
				onclick={handleRefresh}
			>
				<svg class="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
					/>
				</svg>
				Refresh
			</button>
		</div>
	</div>

	<!-- Error State -->
	{#if $autoscaleStore.error}
		<div class="card p-4 preset-outlined-error-500">
			<p class="text-error-500">{$autoscaleStore.error}</p>
		</div>
	{/if}

	<!-- Providers Section -->
	<section class="space-y-4">
		<div class="flex items-center justify-between">
			<h2 class="text-xl font-semibold">Providers</h2>
			{#if !$autoscaleStore.featureDisabled}
				<button type="button" class="rq-btn-primary" onclick={openCreateProvider}>
					<svg class="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
					</svg>
					Add Provider
				</button>
			{/if}
		</div>

		{#if $autoscaleStore.featureDisabled}
			<div class="card preset-outlined-surface-200-800 bg-surface-50-950 p-8 text-center">
				<svg class="w-16 h-16 mx-auto text-warning-500 mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
				</svg>
				<h3 class="text-xl font-semibold mb-2">Provider Management Disabled</h3>
				<p class="text-surface-500 mb-4">
					Autoscaling provider management is not configured on this server.
				</p>
				<p class="text-sm text-surface-400">
					To enable it, set the <code class="bg-surface-200 dark:bg-surface-700 px-2 py-1 rounded">RUNQY_VAULT_MASTER_KEY</code> environment variable on the server.
				</p>
			</div>
		{:else if $autoscaleStore.providers.length === 0}
			<div class="card preset-outlined-surface-200-800 bg-surface-50-950 p-8 text-center">
				<svg class="w-12 h-12 mx-auto text-surface-400 mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01"/>
				</svg>
				<p class="text-surface-500 mb-4">No providers configured</p>
				<button type="button" class="btn preset-filled-primary-500" onclick={openCreateProvider}>
					<svg class="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
					</svg>
					Add your first provider
				</button>
			</div>
		{:else}
			<div class="table-container">
				<table class="table table-hover">
					<thead>
						<tr>
							<th>Name</th>
							<th>Type</th>
							<th>Status</th>
							<th>Updated</th>
							<th>Actions</th>
						</tr>
					</thead>
					<tbody>
						{#each $autoscaleStore.providers as provider (provider.name)}
							<tr>
								<td class="font-mono font-medium">{provider.name}</td>
								<td>{provider.provider_type}</td>
								<td>
									{#if provider.enabled}
										<span class="badge preset-filled-success-500 text-xs">Enabled</span>
									{:else}
										<span class="badge preset-outlined-surface-500 text-xs">Disabled</span>
									{/if}
								</td>
								<td class="text-sm text-surface-500">{formatRelativeTime(provider.updated_at)}</td>
								<td>
									<div class="flex items-center gap-1">
										<button
											type="button"
											class="btn btn-sm preset-outlined-primary-500"
											onclick={() => openEditProvider(provider)}
											title="Edit provider"
										>
											<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
												<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
											</svg>
										</button>
										<button
											type="button"
											class="btn btn-sm preset-outlined-error-500"
											onclick={() => confirmDeleteProvider(provider.name)}
											title="Delete provider"
										>
											<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
												<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
											</svg>
										</button>
									</div>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>

	<!-- Queue Autoscale Configs Section -->
	<section class="space-y-4">
		<div class="flex items-center justify-between">
			<h2 class="text-xl font-semibold">Queues</h2>
			<a href="{base}/queues" class="text-sm text-surface-500 hover:underline">All queues →</a>
		</div>

		{#if queueRows.length === 0}
			<div class="card preset-outlined-surface-200-800 bg-surface-50-950 p-8 text-center">
				<p class="text-surface-500">No queue has an autoscale configuration</p>
				<p class="text-sm text-surface-400 mt-1">
					Add an <code>autoscale</code> block to a queue (YAML or the queue's config editor).
				</p>
			</div>
		{:else}
			<div class="table-container">
				<table class="table table-hover">
					<thead>
						<tr>
							<th>Queue</th>
							<th>Status</th>
							<th>Provider</th>
							<th>Workers</th>
							<th>Scale up</th>
							<th>Scale down</th>
							<th>Instance</th>
							<th>Actions</th>
						</tr>
					</thead>
					<tbody>
						{#each queueRows as row (row.parent)}
							{@const asc = row.autoscale}
							{@const prov = providerFor(asc.provider)}
							<tr>
								<td class="font-mono font-medium">
									<a href="{base}/queues/{encodeURIComponent(row.configName)}" class="hover:underline">{row.parent}</a>
								</td>
								<td>
									{#if asc.enabled}
										<span class="badge preset-filled-success-500 text-xs">Enabled</span>
									{:else}
										<span class="badge preset-outlined-surface-500 text-xs">Disabled</span>
									{/if}
									{#each pausesFor(row.parent) as p (p.kind)}
										<div class="mt-1" title="{p.failures} failure(s) in a row: {p.cause}. Provisioning resumes automatically at that time.">
											<span class="badge preset-filled-warning-500 text-xs whitespace-nowrap">paused until {formatUntil(p.until)}</span>
											<div class="text-xs text-surface-400">{p.failures}× {p.cause}</div>
										</div>
									{/each}
								</td>
								<td>
									<div class="font-mono text-sm">{asc.provider || '-'}</div>
									{#if !$autoscaleStore.featureDisabled}
										{#if !prov}
											<span class="badge preset-filled-error-500 text-xs" title="No provider with this name is registered">unknown provider</span>
										{:else}
											<div class="flex flex-wrap items-center gap-1 mt-1">
												<span class="text-xs text-surface-400">{prov.provider_type}</span>
												{#if prov.provider_type === 'mock'}
													<span class="badge preset-filled-warning-500 text-xs" title="The mock provider rents no machine: no worker will ever boot">mock: boots nothing</span>
												{/if}
												{#if !prov.enabled}
													<span class="badge preset-outlined-warning-500 text-xs">provider disabled</span>
												{/if}
											</div>
										{/if}
									{/if}
								</td>
								<td class="text-sm whitespace-nowrap">
									<span class="font-semibold">{liveCount(row.parent)}</span>
									<span class="text-surface-400">live · {asc.min_workers}–{asc.max_workers}</span>
									{#if asc.boot_timeout}
										<div class="text-xs text-surface-400">boot timeout {asc.boot_timeout}</div>
									{/if}
								</td>
								<td class="text-sm">
									{#each asc.scale_up || [] as t, idx (idx)}
										<div>{describeTrigger(t, 'up')}</div>
									{:else}
										<span class="text-surface-400">-</span>
									{/each}
								</td>
								<td class="text-sm">
									{#each asc.scale_down || [] as t, idx (idx)}
										<div>{describeTrigger(t, 'down')}</div>
									{:else}
										<span class="text-surface-400">-</span>
									{/each}
								</td>
								<td class="text-sm">
									{#each describeInstance(asc) as part, idx (idx)}
										<div class={idx === 0 ? '' : 'text-xs text-surface-400'}>{part}</div>
									{:else}
										<span class="text-surface-400">provider defaults</span>
									{/each}
								</td>
								<td>
									<button
										type="button"
										class="btn btn-sm preset-outlined-primary-500"
										onclick={() => openEditQueueConfig(row.configName)}
										title="Edit queue config"
									>
										<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
										</svg>
									</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>

	<!-- Instances Section -->
	<section class="space-y-4">
		<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
			<h2 class="text-xl font-semibold">Instances</h2>
			<div class="flex flex-wrap items-center gap-4">
				<SegmentedControl
					name="instanceFilter"
					value={instanceFilter}
					onValueChange={(e) => setFilter(e.value as InstanceFilter)}
				>
					<SegmentedControl.Control class="inline-flex gap-1 p-1 rounded-base preset-outlined-surface-200-800">
						{#each [['active', 'Active'], ['failed', 'Failed'], ['terminated', 'Terminated'], ['all', 'All']] as [value, label] (value)}
							<SegmentedControl.Item
								{value}
								class="px-3 py-1 rounded-base text-sm cursor-pointer text-surface-600-400 hover:preset-tonal data-[state=checked]:preset-filled-primary-500"
							>
								<SegmentedControl.ItemText>{label} ({counts[value as InstanceFilter]})</SegmentedControl.ItemText>
								<SegmentedControl.ItemHiddenInput />
							</SegmentedControl.Item>
						{/each}
					</SegmentedControl.Control>
				</SegmentedControl>
				<div class="text-sm text-surface-500">
					Total Cost: <span class="font-semibold text-surface-700 dark:text-surface-300">{formatCost($autoscaleStore.totalCost)}</span>
				</div>
			</div>
		</div>

		{#if visibleInstances.length === 0}
			<div class="card preset-outlined-surface-200-800 bg-surface-50-950 p-8 text-center">
				<svg class="w-12 h-12 mx-auto text-surface-400 mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 7h8m0 0v8m0-8l-8 8-4-4-6 6"/>
				</svg>
				<p class="text-surface-500">
					{instanceFilter === 'active' ? 'No active instances' : `No ${instanceFilter === 'all' ? '' : instanceFilter + ' '}instances`}
				</p>
				{#if instanceFilter === 'active'}
					<p class="text-sm text-surface-400 mt-1">
						Instances will appear here when autoscaling provisions workers.
					</p>
				{/if}
			</div>
		{:else}
			<div class="table-container">
				<table class="table table-hover">
					<thead>
						<tr>
							<th>Instance ID</th>
							<th>Queue</th>
							<th>Provider</th>
							<th>Status</th>
							<th>Worker</th>
							<th>Protected</th>
							<th>Cost</th>
							<th>Created</th>
							<th>Actions</th>
						</tr>
					</thead>
					<tbody>
						{#each visibleInstances as instance (instance.instance_id)}
							{@const reason = instanceReason(instance)}
							{@const isOpen = expanded.has(instance.instance_id)}
							<tr class="cursor-pointer" onclick={() => toggleExpanded(instance.instance_id)} title="Show details">
								<td class="font-mono text-sm">
									<span class="text-surface-400 mr-1">{isOpen ? '▾' : '▸'}</span>{instance.instance_id}
								</td>
								<td>{instance.queue}</td>
								<td>
									{instance.provider}
									{#if instance.gpu}
										<div class="text-xs text-surface-400">{instance.gpu}</div>
									{/if}
								</td>
								<td>
									<span class={statusBadgeClass(displayStatus(instance))}>{displayStatus(instance)}</span>
									{#if reason && !isOpen}
										<div class="text-xs text-surface-400 truncate max-w-xs" title={reason}>{reason}</div>
									{/if}
								</td>
								<td class="font-mono text-sm">
									{#if instance.worker_id}
										<a
											href="{base}/workers/{encodeURIComponent(instance.worker_id)}"
											class="hover:underline"
											onclick={(e) => e.stopPropagation()}
										>{instance.worker_id}</a>
									{:else}
										<span class="text-surface-400">-</span>
									{/if}
								</td>
								<td>
									{#if instance.protected}
										<span class="badge preset-filled-warning-500 text-xs">Protected</span>
									{:else}
										<span class="badge preset-outlined-surface-500 text-xs">No</span>
									{/if}
								</td>
								<td class="font-mono text-sm">
									{formatCost(instance.cost_accumulated)}
									<span class="text-xs text-surface-400">({formatCost(instance.price_per_hour)}/hr)</span>
								</td>
								<td class="text-sm text-surface-500">{formatRelativeTime(instance.created_at)}</td>
								<td>
									{#if isLiveInstance(instance)}
										<button
											type="button"
											class="btn btn-sm {instance.protected ? 'preset-outlined-surface-500' : 'preset-outlined-warning-500'}"
											onclick={(e) => { e.stopPropagation(); handleToggleProtect(instance); }}
											title={instance.protected ? 'Unprotect instance' : 'Protect instance'}
										>
											{instance.protected ? 'Unprotect' : 'Protect'}
										</button>
									{:else}
										<span class="text-xs text-surface-400">-</span>
									{/if}
								</td>
							</tr>
							{#if isOpen}
								<tr>
									<td colspan="9" class="bg-surface-50-950">
										<dl class="grid grid-cols-1 md:grid-cols-[max-content_1fr] gap-x-6 gap-y-2 text-sm p-2">
											<dt class="text-surface-500">Provider instance</dt>
											<dd class="font-mono">{instance.provider_instance_id || '- (no machine rented)'}</dd>
											<dt class="text-surface-500">{instance.error ? 'Provider error' : 'Reason'}</dt>
											<dd class="font-mono whitespace-pre-wrap break-words">{reason || '-'}</dd>
											<dt class="text-surface-500">Created</dt>
											<dd>{formatDateTime(instance.created_at)}</dd>
											<dt class="text-surface-500">Last update</dt>
											<dd>{formatDateTime(instance.updated_at)}</dd>
											{#if instance.last_job_at}
												<dt class="text-surface-500">Last task</dt>
												<dd>{formatDateTime(instance.last_job_at)}</dd>
											{/if}
											{#if instance.worker_id}
												<dt class="text-surface-500">Worker</dt>
												<dd>
													<a href="{base}/workers/{encodeURIComponent(instance.worker_id)}" class="font-mono hover:underline">{instance.worker_id}</a>
													<span class="text-surface-400">(live process logs on the worker page)</span>
												</dd>
											{/if}
											{#if instance.provider_instance_id}
												{@const ls = logsById[instance.instance_id]}
												<dt class="text-surface-500">Machine logs</dt>
												<dd class="space-y-2 min-w-0">
													<button
														type="button"
														class="btn btn-sm preset-outlined-primary-500"
														disabled={ls?.loading}
														onclick={() => loadLogs(instance)}
													>
														{#if ls?.loading}
															Fetching…
														{:else if isEnded(instance)}
															{ls?.data ? 'Reload saved logs' : 'Show saved logs'}
														{:else}
															{ls?.data ? 'Refresh' : 'Fetch live logs'}
														{/if}
													</button>
													{#if ls?.error}
														<p class="text-error-500">{ls.error}</p>
													{:else if ls?.data}
														{#if ls.data.logs}
															<p class="text-xs text-surface-400">
																{ls.data.source === 'live' ? 'Fetched from the provider' : 'Saved when the instance was destroyed'}
																{#if ls.data.fetched_at}· {formatDateTime(ls.data.fetched_at)}{/if}
															</p>
															<pre class="card preset-outlined-surface-200-800 bg-surface-100-900 p-3 text-xs font-mono whitespace-pre-wrap break-all max-h-96 overflow-auto">{ls.data.logs}</pre>
														{:else if ls.data.error}
															<p class="text-warning-500">Logs could not be captured before teardown: {ls.data.error}</p>
														{:else}
															<p class="text-surface-400">
																{ls.data.source === 'live' ? 'The machine has produced no output yet.' : 'No logs were saved for this instance (they are captured only for instances that fail after a machine was rented).'}
															</p>
														{/if}
													{/if}
												</dd>
											{/if}
										</dl>
									</td>
								</tr>
							{/if}
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>
</div>

<ProviderModal
	bind:open={providerModalOpen}
	loading={providerModalLoading}
	mode={providerModalMode}
	provider={selectedProvider}
	{providerTypes}
	onsave={handleSaveProvider}
/>

<QueueConfigModal
	bind:open={queueConfigModalOpen}
	loading={queueConfigModalLoading}
	mode="edit"
	config={selectedQueueConfig}
	onsave={handleSaveQueueConfig}
/>

<ConfirmDialog
	bind:open={confirmDialog.open}
	title={confirmDialog.title}
	message={confirmDialog.message}
	variant="danger"
	confirmText="Delete"
	onconfirm={confirmDialog.action}
/>
