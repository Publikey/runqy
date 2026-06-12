<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { base } from '$app/paths';
	import { vaultsStore } from '$lib/stores/vaults';
	import { settings } from '$lib/stores/settings';
	import { toaster } from '$lib/stores/toaster';
	import { createVault, deleteVault } from '$lib/api/client';
	import VaultCard from '$lib/components/VaultCard.svelte';
	import CreateVaultModal from '$lib/components/CreateVaultModal.svelte';
	import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';

	let pollInterval: ReturnType<typeof setInterval> | null = null;
	let searchQuery = $state('');
	let refreshing = $state(false);
	let createModalOpen = $state(false);
	let createLoading = $state(false);
	let confirmDialog = $state({
		open: false,
		title: '',
		message: '',
		vaultName: '',
		action: () => {}
	});

	let filteredVaults = $derived(
		$vaultsStore.vaults.filter((v) => {
			if (searchQuery && !v.name.toLowerCase().includes(searchQuery.toLowerCase()) &&
				!v.description.toLowerCase().includes(searchQuery.toLowerCase())) {
				return false;
			}
			return true;
		})
	);

	async function loadData() {
		await vaultsStore.fetch();
	}

	async function handleRefresh() {
		refreshing = true;
		await loadData();
		setTimeout(() => { refreshing = false; }, 500);
	}

	onMount(() => {
		loadData();
		pollInterval = setInterval(loadData, $settings.pollInterval * 1000);
	});

	onDestroy(() => {
		if (pollInterval) clearInterval(pollInterval);
	});

	function navigateToVault(name: string) {
		goto(`${base}/vaults/${encodeURIComponent(name)}`);
	}

	async function handleCreateVault(name: string, description: string) {
		createLoading = true;
		try {
			await createVault(name, description);
			await loadData();
			toaster.success({ title: 'Vault Created', description: `Vault "${name}" created` });
			createModalOpen = false;
		} catch (e) {
			const errorMessage = e instanceof Error ? e.message : 'Failed to create vault';
			toaster.error({ title: 'Error', description: errorMessage });
		} finally {
			createLoading = false;
		}
	}

	function confirmDeleteVault(name: string) {
		confirmDialog = {
			open: true,
			title: 'Delete Vault',
			message: `Are you sure you want to delete vault "${name}"? This will permanently remove all entries in this vault. This action cannot be undone.`,
			vaultName: name,
			action: async () => {
				try {
					await deleteVault(name);
					await loadData();
					toaster.success({ title: 'Vault Deleted', description: `Vault "${name}" deleted` });
				} catch (e) {
					const errorMessage = e instanceof Error ? e.message : 'Failed to delete vault';
					toaster.error({ title: 'Error', description: errorMessage });
				}
			}
		};
	}

	function handleSearchInput(e: Event) {
		searchQuery = (e.target as HTMLInputElement).value;
	}
</script>

<svelte:head>
	<title>Vaults - runqy Monitor</title>
</svelte:head>

<div class="p-4 md:p-6 space-y-6">
	<!-- Header -->
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<h1 class="text-2xl font-bold">Vaults</h1>
			<p class="text-surface-500">
				{filteredVaults.length} vault{filteredVaults.length !== 1 ? 's' : ''}
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
			{#if !$vaultsStore.featureDisabled}
				<button
					type="button"
					class="rq-btn-primary"
					onclick={() => createModalOpen = true}
				>
					<svg class="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
					</svg>
					Create Vault
				</button>
			{/if}
		</div>
	</div>

	<!-- Feature Disabled State -->
	{#if $vaultsStore.featureDisabled}
		<div class="card preset-outlined-surface-200-800 bg-surface-50-950 p-8 text-center">
			<svg class="w-16 h-16 mx-auto text-warning-500 mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
			</svg>
			<h3 class="text-xl font-semibold mb-2">Vaults Feature Disabled</h3>
			<p class="text-surface-500 mb-4">
				The vaults feature is not configured on this server.
			</p>
			<p class="text-sm text-surface-400">
				To enable vaults, set the <code class="bg-surface-200 dark:bg-surface-700 px-2 py-1 rounded">RUNQY_VAULT_MASTER_KEY</code> environment variable on the server.
			</p>
		</div>
	{:else}
		<!-- Search -->
		<div class="flex flex-wrap items-center gap-4">
			<div class="relative flex-1 min-w-[200px] max-w-md">
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
					value={searchQuery}
					oninput={handleSearchInput}
					placeholder="Search vaults..."
					class="input pl-10 pr-4 py-2 w-full"
				/>
			</div>
		</div>

		<!-- Error State -->
		{#if $vaultsStore.error && !$vaultsStore.featureDisabled}
			<div class="card p-4 preset-outlined-error-500">
				<p class="text-error-500">Failed to load vaults: {$vaultsStore.error}</p>
			</div>
		{/if}

		<!-- Loading State -->
		{#if $vaultsStore.loading && filteredVaults.length === 0}
			<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
				{#each [1, 2, 3, 4, 5, 6] as i (i)}
					<div class="card preset-outlined-surface-200-800 bg-surface-50-950 p-4">
						<div class="animate-pulse space-y-3">
							<div class="h-6 bg-surface-300 dark:bg-surface-600 rounded w-1/2"></div>
							<div class="h-4 bg-surface-300 dark:bg-surface-600 rounded w-3/4"></div>
							<div class="h-4 bg-surface-300 dark:bg-surface-600 rounded w-1/4"></div>
						</div>
					</div>
				{/each}
			</div>
		{:else if filteredVaults.length === 0}
			<div class="card preset-outlined-surface-200-800 bg-surface-50-950 p-8 text-center">
				<svg class="w-12 h-12 mx-auto text-surface-400 mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z"/>
				</svg>
				<p class="text-surface-500 mb-4">
					{searchQuery ? 'No vaults match your search' : 'No vaults found'}
				</p>
				{#if !searchQuery}
					<button
						type="button"
						class="btn preset-filled-primary-500"
						onclick={() => createModalOpen = true}
					>
						<svg class="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
						</svg>
						Create your first vault
					</button>
				{/if}
			</div>
		{:else}
			<!-- Vault Grid -->
			<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
				{#each filteredVaults as vault (vault.name)}
					<VaultCard
						{vault}
						onClick={() => navigateToVault(vault.name)}
						onDelete={() => confirmDeleteVault(vault.name)}
					/>
				{/each}
			</div>
		{/if}
	{/if}
</div>

<CreateVaultModal
	bind:open={createModalOpen}
	loading={createLoading}
	oncreate={handleCreateVault}
/>

<ConfirmDialog
	bind:open={confirmDialog.open}
	title={confirmDialog.title}
	message={confirmDialog.message}
	variant="danger"
	confirmText="Delete"
	onconfirm={confirmDialog.action}
/>
