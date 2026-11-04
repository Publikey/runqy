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
	import { KeyRound, Plus, Search, TriangleAlert } from '@lucide/svelte';
	import { Callout, Button, EmptyState, PageHeader, Shimmer } from '$lib/components/ui';

	let pollInterval: ReturnType<typeof setInterval> | null = null;
	let searchQuery = $state('');
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

<div class="rq-page space-y-6">
	<PageHeader
		title="Vaults"
		subtitle="{filteredVaults.length} vault{filteredVaults.length !== 1 ? 's' : ''}"
		onrefresh={loadData}
	>
		{#snippet actions()}
			{#if !$vaultsStore.featureDisabled}
				<Button variant="primary" icon={Plus} onclick={() => (createModalOpen = true)}>Create vault</Button>
			{/if}
		{/snippet}
	</PageHeader>

	{#if $vaultsStore.featureDisabled}
		<div class="rq-card">
			<EmptyState
				icon={TriangleAlert}
				title="Vaults feature disabled"
				description="The vaults feature is not configured on this server."
			>
				{#snippet action()}
					<p class="text-ui text-fg-subtle">
						To enable vaults, set the <code class="font-mono text-fg">RUNQY_VAULT_MASTER_KEY</code> environment variable on the server.
					</p>
				{/snippet}
			</EmptyState>
		</div>
	{:else}
		<!-- Search -->
		<div class="relative w-full max-w-md">
			<Search
				size={16}
				strokeWidth={1.75}
				class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-fg-subtle"
			/>
			<input
				type="text"
				value={searchQuery}
				oninput={handleSearchInput}
				placeholder="Search vaults..."
				aria-label="Search vaults"
				class="input pl-9 w-full"
			/>
		</div>

		{#if $vaultsStore.error && !$vaultsStore.featureDisabled}
			<Callout>
				Failed to load vaults: {$vaultsStore.error}
			</Callout>
		{/if}

		{#if $vaultsStore.loading && filteredVaults.length === 0}
			<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
				{#each [1, 2, 3, 4, 5, 6] as i (i)}
					<div class="rq-card p-4 space-y-3">
						<Shimmer class="h-5 w-1/2" />
						<Shimmer class="h-4 w-3/4" />
						<Shimmer class="h-4 w-1/4" />
					</div>
				{/each}
			</div>
		{:else if filteredVaults.length === 0}
			<div class="rq-card">
				<EmptyState icon={KeyRound} title={searchQuery ? 'No vaults match your search' : 'No vaults found'}>
					{#snippet action()}
						{#if !searchQuery}
							<Button variant="secondary" icon={Plus} onclick={() => (createModalOpen = true)}>
								Create your first vault
							</Button>
						{/if}
					{/snippet}
				</EmptyState>
			</div>
		{:else}
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
