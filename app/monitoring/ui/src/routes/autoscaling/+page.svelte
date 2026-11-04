<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { base } from '$app/paths';
	import {
		ArrowRight,
		ChevronDown,
		ChevronRight,
		Cloud,
		Layers,
		Pencil,
		Plus,
		ScrollText,
		Server,
		Shield,
		ShieldOff,
		Trash2,
		TriangleAlert
	} from '@lucide/svelte';
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
		QueueConfigDetail
	} from '$lib/api/types';
	import { formatRelativeTime, formatDateTime } from '$lib/utils/format';
	import { parseQueueName } from '$lib/utils/queueGrouping';
	import ProviderModal from '$lib/components/ProviderModal.svelte';
	import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
	import QueueConfigModal from '$lib/components/QueueConfigModal.svelte';
	import { Callout,
		Badge,
		Button,
		EmptyState,
		IconButton,
		KeyValue,
		PageHeader,
		Panel,
		Segmented
	} from '$lib/components/ui';
	import { instanceStatus } from '$lib/utils/status';
	import { ACTIVE_INSTANCE_STATUSES, describeInstance, describeTrigger, formatUntil } from '$lib/utils/autoscale';

	let pollInterval: ReturnType<typeof setInterval> | null = null;

	// ---- Instances: filter + details ----

	type InstanceFilter = 'active' | 'failed' | 'terminated' | 'all';
	const FILTER_KEY = 'runqy.autoscaling.instanceFilter';

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
				return ACTIVE_INSTANCE_STATUSES.includes(i.status);
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
			(i) => i.queue === parent && ACTIVE_INSTANCE_STATUSES.includes(i.status)
		).length;
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

	function formatCost(value: number): string {
		return `$${(value ?? 0).toFixed(2)}`;
	}

	async function loadData() {
		await Promise.all([autoscaleStore.fetch(), loadQueueConfigs()]);
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

<div class="rq-page space-y-4 md:space-y-6">
	<PageHeader
		title="Autoscaling"
		subtitle="{counts.active} active instance{counts.active !== 1 ? 's' : ''}"
		updatedAt={$autoscaleStore.lastUpdated}
		onrefresh={loadData}
	/>

	{#if $autoscaleStore.error}
		<Callout>
			{$autoscaleStore.error}
		</Callout>
	{/if}

	<!-- Providers -->
	<Panel title="Providers" flush>
		{#snippet actions()}
			{#if !$autoscaleStore.featureDisabled}
				<Button variant="primary" size="sm" icon={Plus} onclick={openCreateProvider}>Add provider</Button>
			{/if}
		{/snippet}

		{#if $autoscaleStore.featureDisabled}
			<EmptyState
				icon={TriangleAlert}
				title="Provider management disabled"
				description="Autoscaling provider management is not configured on this server."
			>
				{#snippet action()}
					<p class="text-ui text-fg-subtle">
						To enable it, set the <code class="font-mono text-fg">RUNQY_VAULT_MASTER_KEY</code> environment variable on the server.
					</p>
				{/snippet}
			</EmptyState>
		{:else if $autoscaleStore.providers.length === 0}
			<EmptyState icon={Cloud} title="No providers configured" compact>
				{#snippet action()}
					<Button variant="secondary" size="sm" icon={Plus} onclick={openCreateProvider}>Add your first provider</Button>
				{/snippet}
			</EmptyState>
		{:else}
			<div class="table-container">
				<table class="rq-table">
					<thead>
						<tr>
							<th>Name</th>
							<th>Type</th>
							<th>Status</th>
							<th>Updated</th>
							<th class="text-right">Actions</th>
						</tr>
					</thead>
					<tbody>
						{#each $autoscaleStore.providers as provider (provider.name)}
							<tr>
								<td class="font-mono font-medium text-fg">{provider.name}</td>
								<td>{provider.provider_type}</td>
								<td>
									{#if provider.enabled}
										<Badge tone="success" dot>Enabled</Badge>
									{:else}
										<Badge tone="outline">Disabled</Badge>
									{/if}
								</td>
								<td class="text-fg-subtle whitespace-nowrap">{formatRelativeTime(provider.updated_at)}</td>
								<td>
									<div class="flex items-center justify-end gap-1">
										<IconButton icon={Pencil} label="Edit provider" onclick={() => openEditProvider(provider)} />
										<IconButton
											icon={Trash2}
											label="Delete provider"
											danger
											onclick={() => confirmDeleteProvider(provider.name)}
										/>
									</div>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</Panel>

	<!-- Queue autoscale configs -->
	<Panel title="Queues" flush>
		{#snippet actions()}
			<Button variant="ghost" size="sm" href="{base}/queues" icon={ArrowRight}>All queues</Button>
		{/snippet}

		{#if queueRows.length === 0}
			<EmptyState
				icon={Layers}
				title="No queue has an autoscale configuration"
				description="Add an autoscale block to a queue (YAML or the queue's config editor)."
				compact
			/>
		{:else}
			<div class="table-container">
				<table class="rq-table">
					<thead>
						<tr>
							<th>Queue</th>
							<th>Status</th>
							<th>Provider</th>
							<th>Workers</th>
							<th>Scale up</th>
							<th>Scale down</th>
							<th>Instance</th>
							<th class="text-right">Actions</th>
						</tr>
					</thead>
					<tbody>
						{#each queueRows as row (row.parent)}
							{@const asc = row.autoscale}
							{@const prov = providerFor(asc.provider)}
							<tr>
								<td class="font-mono font-medium">
									<a
										href="{base}/queues/{encodeURIComponent(row.configName)}"
										class="text-primary-400 hover:text-primary-300">{row.parent}</a
									>
								</td>
								<td>
									{#if asc.enabled}
										<Badge tone="success" dot>Enabled</Badge>
									{:else}
										<Badge tone="outline">Disabled</Badge>
									{/if}
									{#each pausesFor(row.parent) as p (p.kind)}
										<div
											class="mt-1.5"
											title="{p.failures} failure(s) in a row: {p.cause}. Provisioning resumes automatically at that time."
										>
											<Badge tone="warning" class="whitespace-nowrap">paused until {formatUntil(p.until)}</Badge>
											<div class="mt-0.5 text-xs text-fg-subtle">{p.failures}× {p.cause}</div>
										</div>
									{/each}
								</td>
								<td>
									<div class="font-mono">{asc.provider || '-'}</div>
									{#if !$autoscaleStore.featureDisabled}
										{#if !prov}
											<Badge tone="danger" class="mt-1" title="No provider with this name is registered">unknown provider</Badge>
										{:else}
											<div class="mt-1 flex flex-wrap items-center gap-1">
												<span class="text-xs text-fg-subtle">{prov.provider_type}</span>
												{#if prov.provider_type === 'mock'}
													<Badge tone="warning" title="The mock provider rents no machine: no worker will ever boot">mock: boots nothing</Badge>
												{/if}
												{#if !prov.enabled}
													<Badge tone="warning">provider disabled</Badge>
												{/if}
											</div>
										{/if}
									{/if}
								</td>
								<td class="whitespace-nowrap">
									<span class="font-semibold text-fg tabular-nums">{liveCount(row.parent)}</span>
									<span class="text-fg-subtle">live · {asc.min_workers}–{asc.max_workers}</span>
									{#if asc.boot_timeout}
										<div class="text-xs text-fg-subtle">boot timeout {asc.boot_timeout}</div>
									{/if}
								</td>
								<td>
									{#each asc.scale_up || [] as t, idx (idx)}
										<div>{describeTrigger(t, 'up')}</div>
									{:else}
										<span class="text-fg-faint">-</span>
									{/each}
								</td>
								<td>
									{#each asc.scale_down || [] as t, idx (idx)}
										<div>{describeTrigger(t, 'down')}</div>
									{:else}
										<span class="text-fg-faint">-</span>
									{/each}
								</td>
								<td>
									{#each describeInstance(asc) as part, idx (idx)}
										<div class={idx === 0 ? '' : 'text-xs text-fg-subtle'}>{part}</div>
									{:else}
										<span class="text-fg-subtle">provider defaults</span>
									{/each}
								</td>
								<td>
									<div class="flex justify-end">
										<IconButton
											icon={Pencil}
											label="Edit queue config"
											onclick={() => openEditQueueConfig(row.configName)}
										/>
									</div>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</Panel>

	<!-- Instances -->
	<Panel title="Instances" flush>
		{#snippet actions()}
			<Segmented
				name="instanceFilter"
				label="Instance filter"
				items={[
					{ value: 'active', label: 'Active', count: counts.active },
					{ value: 'failed', label: 'Failed', count: counts.failed },
					{ value: 'terminated', label: 'Terminated', count: counts.terminated },
					{ value: 'all', label: 'All', count: counts.all }
				]}
				value={instanceFilter}
				onchange={(v) => setFilter(v as InstanceFilter)}
			/>
			<span class="text-ui text-fg-subtle whitespace-nowrap">
				Total cost <span class="font-medium text-fg tabular-nums">{formatCost($autoscaleStore.totalCost)}</span>
			</span>
		{/snippet}

		{#if visibleInstances.length === 0}
			<EmptyState
				icon={Server}
				title={instanceFilter === 'active'
					? 'No active instances'
					: `No ${instanceFilter === 'all' ? '' : instanceFilter + ' '}instances`}
				description={instanceFilter === 'active'
					? 'Instances will appear here when autoscaling provisions workers.'
					: undefined}
				compact
			/>
		{:else}
			<div class="table-container">
				<table class="rq-table">
					<thead>
						<tr>
							<th>Instance ID</th>
							<th>Queue</th>
							<th>Provider</th>
							<th>Status</th>
							<th>Worker</th>
							<th>Protected</th>
							<th class="text-right">Cost</th>
							<th>Created</th>
							<th class="text-right">Actions</th>
						</tr>
					</thead>
					<tbody>
						{#each visibleInstances as instance (instance.instance_id)}
							{@const reason = instanceReason(instance)}
							{@const isOpen = expanded.has(instance.instance_id)}
							{@const ls = logsById[instance.instance_id]}
							<tr
								class="cursor-pointer"
								onclick={() => toggleExpanded(instance.instance_id)}
								title="Show details"
								aria-expanded={isOpen}
							>
								<td class="font-mono whitespace-nowrap">
									<span class="inline-flex items-center gap-1.5">
										{#if isOpen}
											<ChevronDown size={14} strokeWidth={1.75} class="text-fg-subtle" />
										{:else}
											<ChevronRight size={14} strokeWidth={1.75} class="text-fg-subtle" />
										{/if}
										{instance.instance_id}
									</span>
								</td>
								<td class="font-mono">{instance.queue}</td>
								<td>
									{instance.provider}
									{#if instance.gpu}
										<div class="text-xs text-fg-subtle">{instance.gpu}</div>
									{/if}
								</td>
								<td>
									<Badge status={instanceStatus(instance.status, instance.worker_id)} />
									{#if reason && !isOpen}
										<div class="mt-0.5 text-xs text-fg-subtle truncate max-w-xs" title={reason}>{reason}</div>
									{/if}
								</td>
								<td class="font-mono">
									{#if instance.worker_id}
										<a
											href="{base}/workers/{encodeURIComponent(instance.worker_id)}"
											class="text-primary-400 hover:text-primary-300"
											onclick={(e) => e.stopPropagation()}>{instance.worker_id}</a
										>
									{:else}
										<span class="text-fg-faint">-</span>
									{/if}
								</td>
								<td>
									{#if instance.protected}
										<Badge tone="warning">Protected</Badge>
									{:else}
										<span class="text-fg-subtle">No</span>
									{/if}
								</td>
								<td class="text-right tabular-nums font-mono whitespace-nowrap">
									{formatCost(instance.cost_accumulated)}
									<span class="text-xs text-fg-subtle">({formatCost(instance.price_per_hour)}/hr)</span>
								</td>
								<td class="text-fg-subtle whitespace-nowrap">{formatRelativeTime(instance.created_at)}</td>
								<td>
									<div class="flex justify-end">
										{#if isLiveInstance(instance)}
											<Button
												size="sm"
												variant="secondary"
												icon={instance.protected ? ShieldOff : Shield}
												title={instance.protected ? 'Unprotect instance' : 'Protect instance'}
												onclick={(e) => {
													e.stopPropagation();
													handleToggleProtect(instance);
												}}
											>
												{instance.protected ? 'Unprotect' : 'Protect'}
											</Button>
										{:else}
											<span class="text-fg-faint">-</span>
										{/if}
									</div>
								</td>
							</tr>
							{#if isOpen}
								{#snippet reasonValue()}
									<span class="font-mono whitespace-pre-wrap break-words">{reason || '-'}</span>
								{/snippet}
								{#snippet workerValue()}
									<a
										href="{base}/workers/{encodeURIComponent(instance.worker_id ?? '')}"
										class="font-mono text-primary-400 hover:text-primary-300">{instance.worker_id}</a
									>
									<span class="text-fg-subtle">(live process logs on the worker page)</span>
								{/snippet}
								<tr>
									<td colspan="9" class="bg-canvas">
										<div class="space-y-4 py-2">
											<KeyValue
												columns={3}
												items={[
													{
														label: 'Provider instance',
														value: instance.provider_instance_id || '- (no machine rented)',
														mono: true
													},
													{ label: instance.error ? 'Provider error' : 'Reason', render: reasonValue },
													{ label: 'Created', value: formatDateTime(instance.created_at) },
													{ label: 'Last update', value: formatDateTime(instance.updated_at) },
													...(instance.last_job_at
														? [{ label: 'Last task', value: formatDateTime(instance.last_job_at) }]
														: []),
													...(instance.worker_id ? [{ label: 'Worker', render: workerValue }] : [])
												]}
											/>
											{#if instance.provider_instance_id}
												<div class="space-y-2 min-w-0">
													<div class="flex flex-wrap items-center gap-3">
														<span class="text-xs font-medium text-fg-subtle">Machine logs</span>
														<Button
															size="sm"
															variant="secondary"
															icon={ScrollText}
															loading={ls?.loading}
															onclick={() => loadLogs(instance)}
														>
															{#if ls?.loading}
																Fetching…
															{:else if isEnded(instance)}
																{ls?.data ? 'Reload saved logs' : 'Show saved logs'}
															{:else}
																{ls?.data ? 'Refresh' : 'Fetch live logs'}
															{/if}
														</Button>
													</div>
													{#if ls?.error}
														<p class="text-ui text-error-300">{ls.error}</p>
													{:else if ls?.data}
														{#if ls.data.logs}
															<p class="text-xs text-fg-subtle">
																{ls.data.source === 'live' ? 'Fetched from the provider' : 'Saved when the instance was destroyed'}
																{#if ls.data.fetched_at}· {formatDateTime(ls.data.fetched_at)}{/if}
															</p>
															<pre
																class="bg-canvas border border-line-subtle rounded-md p-3 text-xs font-mono whitespace-pre-wrap break-all max-h-96 overflow-auto text-fg-muted">{ls.data.logs}</pre>
														{:else if ls.data.error}
															<p class="text-ui text-warning-300">Logs could not be captured before teardown: {ls.data.error}</p>
														{:else}
															<p class="text-ui text-fg-subtle">
																{ls.data.source === 'live'
																	? 'The machine has produced no output yet.'
																	: 'No logs were saved for this instance (they are captured only for instances that fail after a machine was rented).'}
															</p>
														{/if}
													{/if}
												</div>
											{/if}
										</div>
									</td>
								</tr>
							{/if}
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</Panel>
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
