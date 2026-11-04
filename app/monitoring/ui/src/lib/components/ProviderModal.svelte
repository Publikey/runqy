<script lang="ts">
	import type { AutoscaleProvider } from '$lib/api/types';
	import { Callout, Button, Field, Modal, Toggle } from '$lib/components/ui';

	interface Props {
		open?: boolean;
		loading?: boolean;
		mode?: 'create' | 'edit';
		provider?: AutoscaleProvider | null;
		providerTypes?: string[];
		onsave?: (body: {
			name: string;
			provider_type: string;
			config: Record<string, unknown>;
			enabled: boolean;
		}) => void;
		oncancel?: () => void;
	}

	let {
		open = $bindable(false),
		loading = false,
		mode = 'create',
		provider = null,
		providerTypes = [],
		onsave,
		oncancel
	}: Props = $props();

	let name = $state('');
	let providerType = $state('');
	let enabled = $state(true);
	let configText = $state('{}');
	let error = $state('');

	function handleSubmit() {
		error = '';
		if (mode === 'create') {
			if (!name.trim()) {
				error = 'Provider name is required';
				return;
			}
			if (!/^[a-zA-Z0-9_-]+$/.test(name.trim())) {
				error = 'Provider name can only contain letters, numbers, hyphens, and underscores';
				return;
			}
		}
		if (!providerType) {
			error = 'Provider type is required';
			return;
		}

		let config: Record<string, unknown>;
		try {
			const parsed = configText.trim() ? JSON.parse(configText) : {};
			if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
				error = 'Config must be a JSON object';
				return;
			}
			config = parsed as Record<string, unknown>;
		} catch {
			error = 'Config must be valid JSON';
			return;
		}

		onsave?.({
			name: name.trim(),
			provider_type: providerType,
			config,
			enabled
		});
	}

	// Set while the Cancel button closes the dialog, so onclose does not report it twice.
	let cancelling = false;

	function handleCancel() {
		cancelling = true;
		oncancel?.();
		open = false;
	}

	function handleClose() {
		if (!cancelling) oncancel?.();
		cancelling = false;
	}

	$effect(() => {
		if (open) {
			if (provider && mode === 'edit') {
				name = provider.name;
				providerType = provider.provider_type;
				enabled = provider.enabled;
				configText = JSON.stringify(provider.config ?? {}, null, 2);
			} else {
				name = '';
				providerType = providerTypes[0] ?? '';
				enabled = true;
				configText = '{}';
			}
			error = '';
		}
	});
</script>

<Modal
	bind:open
	title={mode === 'create' ? 'Add provider' : 'Edit provider'}
	onclose={handleClose}
>
	<form
		id="provider-form"
		class="space-y-4"
		onsubmit={(e) => {
			e.preventDefault();
			handleSubmit();
		}}
	>
		<Field label="Name" for="provider-name">
			<input
				id="provider-name"
				type="text"
				bind:value={name}
				placeholder="my-provider"
				class="input"
				disabled={loading || mode === 'edit'}
			/>
		</Field>

		<Field label="Provider type" for="provider-type">
			<select id="provider-type" bind:value={providerType} class="select" disabled={loading}>
				{#if providerTypes.length === 0}
					<option value="">No provider types available</option>
				{:else}
					{#each providerTypes as type (type)}
						<option value={type}>{type}</option>
					{/each}
				{/if}
			</select>
		</Field>

		<Field
			label="Config (JSON)"
			for="provider-config"
			hint={mode === 'edit' ? 'Secret values are masked on read. Re-enter them to update.' : undefined}
		>
			<textarea
				id="provider-config"
				bind:value={configText}
				placeholder={'{\n  "api_key": "..."\n}'}
				class="textarea font-mono text-ui"
				rows={8}
				disabled={loading}
			></textarea>
		</Field>

		<Toggle bind:checked={enabled} label="Enabled" disabled={loading} />

		{#if error}
			<Callout>{error}</Callout>
		{/if}
	</form>

	{#snippet footer()}
		<Button variant="secondary" onclick={handleCancel} disabled={loading}>Cancel</Button>
		<Button variant="primary" type="submit" form="provider-form" {loading}>
			{#if loading}Saving...{:else}{mode === 'create' ? 'Add provider' : 'Update provider'}{/if}
		</Button>
	{/snippet}
</Modal>
