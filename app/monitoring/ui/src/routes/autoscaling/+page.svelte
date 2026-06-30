<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { autoscaleStore } from '$lib/stores/autoscale';
	import { settings } from '$lib/stores/settings';
	import { toaster } from '$lib/stores/toaster';
	import {
		protectInstance,
		unprotectInstance,
		getAutoscaleProviderTypes,
		createAutoscaleProvider,
		updateAutoscaleProvider,
		deleteAutoscaleProvider
	} from '$lib/api/client';
	import type { AutoscaleInstance, AutoscaleProvider } from '$lib/api/types';
	import { formatRelativeTime } from '$lib/utils/format';
	import ProviderModal from '$lib/components/ProviderModal.svelte';
	import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';

	let pollInterval: ReturnType<typeof setInterval> | null = null;
	let refreshing = $state(false);

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

	function isLiveInstance(i: AutoscaleInstance): boolean {
		return i.status !== 'terminated';
	}

	function statusBadgeClass(status: string): string {
		switch (status) {
			case 'running':
				return 'badge preset-filled-success-500 text-xs';
			case 'provisioning':
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
		await autoscaleStore.fetch();
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
				{$autoscaleStore.instances.length} instance{$autoscaleStore.instances.length !== 1 ? 's' : ''}
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

	<!-- Live Status Section -->
	<section class="space-y-4">
		<div class="flex items-center justify-between">
			<h2 class="text-xl font-semibold">Live Instances</h2>
			<div class="text-sm text-surface-500">
				Total Cost: <span class="font-semibold text-surface-700 dark:text-surface-300">{formatCost($autoscaleStore.totalCost)}</span>
			</div>
		</div>

		{#if $autoscaleStore.instances.length === 0}
			<div class="card preset-outlined-surface-200-800 bg-surface-50-950 p-8 text-center">
				<svg class="w-12 h-12 mx-auto text-surface-400 mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 7h8m0 0v8m0-8l-8 8-4-4-6 6"/>
				</svg>
				<p class="text-surface-500">No active instances</p>
				<p class="text-sm text-surface-400 mt-1">
					Instances will appear here when autoscaling provisions workers.
				</p>
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
						{#each $autoscaleStore.instances as instance (instance.instance_id)}
							<tr>
								<td class="font-mono text-sm">{instance.instance_id}</td>
								<td>{instance.queue}</td>
								<td>{instance.provider}</td>
								<td>
									<span class={statusBadgeClass(instance.status)}>{instance.status}</span>
								</td>
								<td class="font-mono text-sm">
									{#if instance.worker_id}
										{instance.worker_id}
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
											onclick={() => handleToggleProtect(instance)}
											title={instance.protected ? 'Unprotect instance' : 'Protect instance'}
										>
											{instance.protected ? 'Unprotect' : 'Protect'}
										</button>
									{:else}
										<span class="text-xs text-surface-400">-</span>
									{/if}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>

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
</div>

<ProviderModal
	bind:open={providerModalOpen}
	loading={providerModalLoading}
	mode={providerModalMode}
	provider={selectedProvider}
	{providerTypes}
	onsave={handleSaveProvider}
/>

<ConfirmDialog
	bind:open={confirmDialog.open}
	title={confirmDialog.title}
	message={confirmDialog.message}
	variant="danger"
	confirmText="Delete"
	onconfirm={confirmDialog.action}
/>
