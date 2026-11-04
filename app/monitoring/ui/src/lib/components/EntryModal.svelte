<script lang="ts">
	import type { VaultEntryView } from '$lib/api/types';
	import { Callout, Button, Field, Modal, Toggle } from '$lib/components/ui';

	interface Props {
		open?: boolean;
		loading?: boolean;
		mode?: 'create' | 'edit';
		entry?: VaultEntryView | null;
		onsave?: (key: string, value: string, isSecret: boolean) => void;
		oncancel?: () => void;
	}

	let {
		open = $bindable(false),
		loading = false,
		mode = 'create',
		entry = null,
		onsave,
		oncancel
	}: Props = $props();

	let key = $state('');
	let value = $state('');
	let isSecret = $state(true);
	let error = $state('');

	function handleSubmit() {
		error = '';
		if (!key.trim()) {
			error = 'Key is required';
			return;
		}
		if (!value.trim()) {
			error = 'Value is required';
			return;
		}
		if (!/^[a-zA-Z0-9_-]+$/.test(key.trim())) {
			error = 'Key can only contain letters, numbers, hyphens, and underscores';
			return;
		}
		onsave?.(key.trim(), value, isSecret);
	}

	// Set while the Cancel button closes the dialog, so onclose does not report it twice.
	let cancelling = false;

	function handleCancel() {
		key = '';
		value = '';
		isSecret = true;
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
		if (open && entry && mode === 'edit') {
			key = entry.key;
			value = '';
			isSecret = entry.is_secret;
		} else if (!open) {
			key = '';
			value = '';
			isSecret = true;
			error = '';
		}
	});
</script>

<Modal bind:open title={mode === 'create' ? 'Add entry' : 'Edit entry'} onclose={handleClose}>
	<form
		id="entry-form"
		class="space-y-4"
		onsubmit={(e) => {
			e.preventDefault();
			handleSubmit();
		}}
	>
		<Field label="Key" for="entry-key">
			<input
				id="entry-key"
				type="text"
				bind:value={key}
				placeholder="API_KEY"
				class="input font-mono"
				disabled={loading || mode === 'edit'}
			/>
		</Field>

		<Field
			label="Value"
			for="entry-value"
			hint={mode === 'edit' ? 'Enter a new value to update this entry' : undefined}
		>
			<textarea
				id="entry-value"
				bind:value
				placeholder={mode === 'edit' ? 'Enter new value...' : 'Enter value...'}
				class="textarea font-mono text-ui"
				rows={4}
				disabled={loading}
			></textarea>
		</Field>

		<Toggle
			bind:checked={isSecret}
			label="Secret value"
			description={isSecret ? 'Value will be masked in UI' : 'Value will be visible in UI'}
			disabled={loading}
		/>

		{#if error}
			<Callout>{error}</Callout>
		{/if}
	</form>

	{#snippet footer()}
		<Button variant="secondary" onclick={handleCancel} disabled={loading}>Cancel</Button>
		<Button variant="primary" type="submit" form="entry-form" {loading}>
			{#if loading}Saving...{:else}{mode === 'create' ? 'Add entry' : 'Update entry'}{/if}
		</Button>
	{/snippet}
</Modal>
