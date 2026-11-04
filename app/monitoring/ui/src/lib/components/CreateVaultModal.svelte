<script lang="ts">
	import { Callout, Button, Field, Modal } from '$lib/components/ui';

	interface Props {
		open?: boolean;
		loading?: boolean;
		oncreate?: (name: string, description: string) => void;
		oncancel?: () => void;
	}

	let {
		open = $bindable(false),
		loading = false,
		oncreate,
		oncancel
	}: Props = $props();

	let name = $state('');
	let description = $state('');
	let error = $state('');

	function handleSubmit() {
		error = '';
		if (!name.trim()) {
			error = 'Vault name is required';
			return;
		}
		if (!/^[a-zA-Z0-9_-]+$/.test(name.trim())) {
			error = 'Vault name can only contain letters, numbers, hyphens, and underscores';
			return;
		}
		oncreate?.(name.trim(), description.trim());
	}

	// Set while the Cancel button closes the dialog, so onclose does not report it twice.
	let cancelling = false;

	function handleCancel() {
		name = '';
		description = '';
		error = '';
		cancelling = true;
		oncancel?.();
		open = false;
	}

	function handleClose() {
		if (!cancelling) oncancel?.();
		cancelling = false;
	}

	$effect(() => {
		if (!open) {
			name = '';
			description = '';
			error = '';
		}
	});
</script>

<Modal bind:open title="Create vault" onclose={handleClose}>
	<form
		id="create-vault-form"
		class="space-y-4"
		onsubmit={(e) => {
			e.preventDefault();
			handleSubmit();
		}}
	>
		<Field label="Name" for="vault-name">
			<input id="vault-name" type="text" bind:value={name} placeholder="my-vault" class="input" disabled={loading} />
		</Field>

		<Field label="Description (optional)" for="vault-description">
			<textarea
				id="vault-description"
				bind:value={description}
				placeholder="Store secrets for production environment..."
				class="textarea"
				rows={3}
				disabled={loading}
			></textarea>
		</Field>

		{#if error}
			<Callout>{error}</Callout>
		{/if}
	</form>

	{#snippet footer()}
		<Button variant="secondary" onclick={handleCancel} disabled={loading}>Cancel</Button>
		<Button variant="primary" type="submit" form="create-vault-form" {loading}>
			{loading ? 'Creating...' : 'Create vault'}
		</Button>
	{/snippet}
</Modal>
