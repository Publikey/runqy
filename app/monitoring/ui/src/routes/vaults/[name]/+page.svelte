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
	import { FileText, Lock, Pencil, Plus, Trash2 } from '@lucide/svelte';
	import { Callout, Badge, Button, EmptyState, IconButton, KeyValue, PageHeader, Panel, Shimmer } from '$lib/components/ui';

	let pollInterval: ReturnType<typeof setInterval> | null = null;
	let vault = $state<VaultDetail | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);

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

	let vaultName = $derived($page.params.name ?? '');

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

<div class="rq-page space-y-4 md:space-y-6">
	<PageHeader
		title={vaultName}
		subtitle={vault?.description || undefined}
		breadcrumbs={[{ label: 'Vaults', href: '/vaults' }, { label: vaultName }]}
		onrefresh={loadData}
	>
		{#snippet titleExtra()}
			{#if vault}
				<Badge tone="outline">{vault.entries?.length || 0} {vault.entries?.length === 1 ? 'entry' : 'entries'}</Badge>
			{/if}
		{/snippet}
		{#snippet actions()}
			<IconButton icon={Trash2} label="Delete vault" variant="secondary" size="md" danger onclick={confirmDeleteVault} />
			<Button variant="primary" icon={Plus} onclick={openAddEntry}>Add entry</Button>
		{/snippet}
	</PageHeader>

	{#if error}
		<Callout>
			Failed to load vault: {error}
		</Callout>
	{/if}

	{#if loading}
		<Panel title="Entries">
			<Shimmer lines={5} />
		</Panel>
	{:else if vault && vault.entries?.length === 0}
		<Panel title="Entries" flush>
			<EmptyState icon={FileText} title="No entries in this vault" compact>
				{#snippet action()}
					<Button variant="secondary" size="sm" icon={Plus} onclick={openAddEntry}>Add your first entry</Button>
				{/snippet}
			</EmptyState>
		</Panel>
	{:else if vault}
		<Panel title="Entries" flush>
			<div class="table-container">
				<table class="rq-table">
					<thead>
						<tr>
							<th>Key</th>
							<th>Value</th>
							<th>Type</th>
							<th>Updated</th>
							<th class="text-right">Actions</th>
						</tr>
					</thead>
					<tbody>
						{#each vault.entries as entry (entry.key)}
							<tr>
								<td class="font-mono font-medium text-fg">{entry.key}</td>
								<td>
									<div class="flex items-center gap-2 min-w-0">
										{#if entry.is_secret}
											<Lock size={14} strokeWidth={1.75} class="shrink-0 text-fg-subtle" />
										{/if}
										<code
											class="font-mono text-xs text-fg-muted bg-raised border border-line-subtle rounded px-1.5 py-0.5 max-w-xs truncate"
											>{entry.value}</code
										>
									</div>
								</td>
								<td>
									{#if entry.is_secret}
										<Badge tone="neutral">Secret</Badge>
									{:else}
										<span class="text-ui text-fg-subtle">Plain</span>
									{/if}
								</td>
								<td class="text-fg-subtle whitespace-nowrap">{formatDate(entry.updated_at)}</td>
								<td>
									<div class="flex items-center justify-end gap-1">
										<IconButton icon={Pencil} label="Edit entry" onclick={() => openEditEntry(entry)} />
										<IconButton icon={Trash2} label="Delete entry" danger onclick={() => confirmDeleteEntry(entry)} />
									</div>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</Panel>

		<Panel title="Vault information">
			<KeyValue
				columns={2}
				items={[
					{ label: 'Created', value: formatDate(vault.created_at) },
					{ label: 'Updated', value: formatDate(vault.updated_at) },
					{ label: 'Total entries', value: vault.entries?.length || 0 },
					{ label: 'Secret entries', value: vault.entries?.filter((e) => e.is_secret).length || 0 }
				]}
			/>
		</Panel>
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
