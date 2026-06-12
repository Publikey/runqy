<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { base } from '$app/paths';
	import { settings } from '$lib/stores/settings';
	import { toaster } from '$lib/stores/toaster';
	import { getVault, setVaultEntry, deleteVaultEntry, deleteVault } from '$lib/api/client';
	import type { VaultDetail, VaultEntryView } from '$lib/api/types';
	import EntryModal from '$lib/components/EntryModal.svelte';
	import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';

	let pollInterval: ReturnType<typeof setInterval> | null = null;
	let vault = $state<VaultDetail | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let refreshing = $state(false);

	let entryModalOpen = $state(false);
	let entryModalMode = $state<'create' | 'edit'>('create');
	let entryModalLoading = $state(false);
	let selectedEntry = $state<VaultEntryView | null>(null);

	let confirmDialog = $state({
		open: false,
		title: '',
		message: '',
		action: () => {}
	});

	let vaultName = $derived($page.params.name);

	async function loadData() {
		try {
			vault = await getVault(vaultName);
			error = null;
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load vault';
		} finally {
			loading = false;
		}
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

	function openAddEntry() {
		selectedEntry = null;
		entryModalMode = 'create';
		entryModalOpen = true;
	}

	function openEditEntry(entry: VaultEntryView) {
		selectedEntry = entry;
		entryModalMode = 'edit';
		entryModalOpen = true;
	}

	async function handleSaveEntry(key: string, value: string, isSecret: boolean) {
		entryModalLoading = true;
		try {
			await setVaultEntry(vaultName, key, value, isSecret);
			await loadData();
			toaster.success({ title: entryModalMode === 'create' ? 'Entry Added' : 'Entry Updated', description: `Entry "${key}" ${entryModalMode === 'create' ? 'added' : 'updated'}` });
			entryModalOpen = false;
		} catch (e) {
			const errorMessage = e instanceof Error ? e.message : 'Failed to save entry';
			toaster.error({ title: 'Error', description: errorMessage });
		} finally {
			entryModalLoading = false;
		}
	}

	function confirmDeleteEntry(entry: VaultEntryView) {
		confirmDialog = {
			open: true,
			title: 'Delete Entry',
			message: `Are you sure you want to delete entry "${entry.key}"? This action cannot be undone.`,
			action: async () => {
				try {
					await deleteVaultEntry(vaultName, entry.key);
					await loadData();
					toaster.success({ title: 'Entry Deleted', description: `Entry "${entry.key}" deleted` });
				} catch (e) {
					const errorMessage = e instanceof Error ? e.message : 'Failed to delete entry';
					toaster.error({ title: 'Error', description: errorMessage });
				}
			}
		};
	}

	function confirmDeleteVault() {
		confirmDialog = {
			open: true,
			title: 'Delete Vault',
			message: `Are you sure you want to delete vault "${vaultName}"? This will permanently remove all entries. This action cannot be undone.`,
			action: async () => {
				try {
					await deleteVault(vaultName);
					toaster.success({ title: 'Vault Deleted', description: `Vault "${vaultName}" deleted` });
					goto(`${base}/vaults`);
				} catch (e) {
					const errorMessage = e instanceof Error ? e.message : 'Failed to delete vault';
					toaster.error({ title: 'Error', description: errorMessage });
				}
			}
		};
	}

	function formatDate(dateStr: string): string {
		try {
			return new Date(dateStr).toLocaleString();
		} catch {
			return dateStr;
		}
	}
</script>

<svelte:head>
	<title>{vaultName} - Vaults - runqy Monitor</title>
</svelte:head>

<div class="p-6 space-y-6">
	<!-- Back Link -->
	<a
		href="{base}/vaults"
		class="inline-flex items-center gap-2 text-surface-500 hover:text-surface-700 dark:hover:text-surface-300"
	>
		<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
			<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
		</svg>
		Back to Vaults
	</a>

	<!-- Header -->
	<div class="flex items-start justify-between">
		<div>
			<div class="flex items-center gap-3 mb-2">
				<svg class="w-8 h-8 text-primary-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z"/>
				</svg>
				<h1 class="text-2xl font-bold">{vaultName}</h1>
			</div>
			{#if vault?.description}
				<p class="text-surface-500">{vault.description}</p>
			{/if}
			{#if vault}
				<p class="text-sm text-surface-400 mt-1">
					{vault.entries?.length || 0} {vault.entries?.length === 1 ? 'entry' : 'entries'}
				</p>
			{/if}
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
			<button
				type="button"
				class="rq-btn-primary"
				onclick={openAddEntry}
			>
				<svg class="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
				</svg>
				Add Entry
			</button>
			<button
				type="button"
				class="btn preset-outlined-error-500"
				onclick={confirmDeleteVault}
				title="Delete vault"
			>
				<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
				</svg>
			</button>
		</div>
	</div>

	<!-- Error State -->
	{#if error}
		<div class="card p-4 preset-outlined-error-500">
			<p class="text-error-500">Failed to load vault: {error}</p>
		</div>
	{/if}

	<!-- Loading State -->
	{#if loading}
		<div class="table-container">
			<table class="table">
				<thead>
					<tr>
						<th>Key</th>
						<th>Value</th>
						<th>Type</th>
						<th>Updated</th>
						<th>Actions</th>
					</tr>
				</thead>
				<tbody>
					{#each [1, 2, 3, 4, 5] as i (i)}
						<tr>
							<td colspan="5">
								<div class="animate-pulse h-4 bg-surface-300 dark:bg-surface-600 rounded"></div>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{:else if vault && vault.entries?.length === 0}
		<div class="card preset-outlined-surface-200-800 bg-surface-50-950 p-8 text-center">
			<svg class="w-12 h-12 mx-auto text-surface-400 mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"/>
			</svg>
			<p class="text-surface-500 mb-4">No entries in this vault</p>
			<button
				type="button"
				class="btn preset-filled-primary-500"
				onclick={openAddEntry}
			>
				<svg class="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
				</svg>
				Add your first entry
			</button>
		</div>
	{:else if vault}
		<!-- Entries Table -->
		<div class="table-container">
			<table class="table table-hover">
				<thead>
					<tr>
						<th>Key</th>
						<th>Value</th>
						<th>Type</th>
						<th>Updated</th>
						<th>Actions</th>
					</tr>
				</thead>
				<tbody>
					{#each vault.entries as entry (entry.key)}
						<tr>
							<td>
								<span class="font-mono font-medium">{entry.key}</span>
							</td>
							<td>
								<div class="flex items-center gap-2">
									{#if entry.is_secret}
										<svg class="w-4 h-4 text-warning-500 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
										</svg>
									{/if}
									<code class="text-sm bg-surface-200 dark:bg-surface-700 px-2 py-1 rounded max-w-xs truncate">
										{entry.value}
									</code>
								</div>
							</td>
							<td>
								{#if entry.is_secret}
									<span class="badge preset-filled-warning-500 text-xs">Secret</span>
								{:else}
									<span class="badge preset-outlined-surface-500 text-xs">Plain</span>
								{/if}
							</td>
							<td class="text-sm text-surface-500">
								{formatDate(entry.updated_at)}
							</td>
							<td>
								<div class="flex items-center gap-1">
									<button
										type="button"
										class="btn btn-sm preset-outlined-primary-500"
										onclick={() => openEditEntry(entry)}
										title="Edit entry"
									>
										<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
										</svg>
									</button>
									<button
										type="button"
										class="btn btn-sm preset-outlined-error-500"
										onclick={() => confirmDeleteEntry(entry)}
										title="Delete entry"
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

		<!-- Vault Metadata -->
		<div class="card preset-outlined-surface-200-800 bg-surface-50-950 p-4">
			<h3 class="text-sm font-medium text-surface-500 mb-2">Vault Information</h3>
			<div class="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
				<div>
					<span class="text-surface-400">Created</span>
					<p class="font-medium">{formatDate(vault.created_at)}</p>
				</div>
				<div>
					<span class="text-surface-400">Updated</span>
					<p class="font-medium">{formatDate(vault.updated_at)}</p>
				</div>
				<div>
					<span class="text-surface-400">Total Entries</span>
					<p class="font-medium">{vault.entries?.length || 0}</p>
				</div>
				<div>
					<span class="text-surface-400">Secret Entries</span>
					<p class="font-medium">{vault.entries?.filter(e => e.is_secret).length || 0}</p>
				</div>
			</div>
		</div>
	{/if}
</div>

<EntryModal
	bind:open={entryModalOpen}
	loading={entryModalLoading}
	mode={entryModalMode}
	entry={selectedEntry}
	onsave={handleSaveEntry}
/>

<ConfirmDialog
	bind:open={confirmDialog.open}
	title={confirmDialog.title}
	message={confirmDialog.message}
	variant="danger"
	confirmText="Delete"
	onconfirm={confirmDialog.action}
/>
