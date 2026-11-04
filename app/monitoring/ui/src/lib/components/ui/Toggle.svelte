<script lang="ts">
	// Labelled on/off switch, built on Skeleton's Switch.
	import { Switch } from '@skeletonlabs/skeleton-svelte';

	interface Props {
		checked?: boolean;
		label: string;
		description?: string;
		disabled?: boolean;
		onchange?: (checked: boolean) => void;
	}

	let { checked = $bindable(false), label, description, disabled = false, onchange }: Props = $props();
</script>

<Switch
	{checked}
	{disabled}
	onCheckedChange={(e) => {
		checked = e.checked;
		onchange?.(checked);
	}}
	class="flex items-start justify-between gap-6"
>
	<div class="min-w-0">
		<Switch.Label class="text-sm font-medium text-fg">{label}</Switch.Label>
		{#if description}<p class="mt-0.5 text-ui text-fg-subtle">{description}</p>{/if}
	</div>
	<Switch.Control class="rq-switch">
		<Switch.Thumb class="rq-switch-thumb" />
	</Switch.Control>
	<Switch.HiddenInput />
</Switch>
