<script lang="ts">
	import type { AutoscaleProvider } from '$lib/api/types';

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

	function handleCancel() {
		oncancel?.();
		open = false;
	}

	function handleBackdropClick(e: MouseEvent) {
		if (e.target === e.currentTarget) {
			handleCancel();
		}
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

{#if open}
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<div
		class="fixed inset-0 z-50 bg-surface-950/70 backdrop-blur-sm flex justify-center items-center p-4 overflow-y-auto"
		onclick={handleBackdropClick}
	>
		<div class="card preset-outlined-surface-200-800 bg-surface-100-900 ring-1 ring-surface-300 dark:ring-surface-600 w-full max-w-md p-6 shadow-xl max-h-[90vh] overflow-y-auto">
			<h2 class="h4 mb-4">
				{mode === 'create' ? 'Add Provider' : 'Edit Provider'}
			</h2>

			<form onsubmit={(e) => { e.preventDefault(); handleSubmit(); }}>
				<div class="space-y-4">
					<label class="label">
						<span class="label-text">Name</span>
						<input
							type="text"
							bind:value={name}
							placeholder="my-provider"
							class="input"
							disabled={loading || mode === 'edit'}
						/>
					</label>

					<label class="label">
						<span class="label-text">Provider Type</span>
						<select bind:value={providerType} class="select" disabled={loading}>
							{#if providerTypes.length === 0}
								<option value="">No provider types available</option>
							{:else}
								{#each providerTypes as type}
									<option value={type}>{type}</option>
								{/each}
							{/if}
						</select>
					</label>

					<label class="label">
						<span class="label-text">Config <span class="text-surface-500">(JSON)</span></span>
						<textarea
							bind:value={configText}
							placeholder={'{\n  "api_key": "..."\n}'}
							class="textarea font-mono text-sm"
							rows={8}
							disabled={loading}
						></textarea>
						{#if mode === 'edit'}
							<span class="label-text text-surface-500">Secret values are masked on read. Re-enter them to update.</span>
						{/if}
					</label>

					<label class="flex items-center gap-3 cursor-pointer">
						<input type="checkbox" bind:checked={enabled} class="checkbox" disabled={loading} />
						<span class="text-sm">Enabled</span>
					</label>

					{#if error}
						<aside class="alert preset-filled-error-500">
							<p>{error}</p>
						</aside>
					{/if}
				</div>

				<footer class="flex justify-end gap-2 mt-6">
					<button type="button" class="btn preset-tonal" onclick={handleCancel} disabled={loading}>
						Cancel
					</button>
					<button type="submit" class="btn preset-filled" disabled={loading}>
						{#if loading}
							Saving...
						{:else}
							{mode === 'create' ? 'Add Provider' : 'Update Provider'}
						{/if}
					</button>
				</footer>
			</form>
		</div>
	</div>
{/if}
