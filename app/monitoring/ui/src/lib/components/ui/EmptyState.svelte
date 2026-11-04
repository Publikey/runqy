<script lang="ts">
	import type { Component, Snippet } from 'svelte';

	interface Props {
		icon?: Component<any>;
		title: string;
		description?: string;
		action?: Snippet;
		/** Smaller padding, for empty tables inside panels. */
		compact?: boolean;
	}

	let { icon: Icon, title, description, action, compact = false }: Props = $props();
</script>

<div class="flex flex-col items-center justify-center text-center {compact ? 'py-8 px-4' : 'py-14 px-6'}">
	{#if Icon}
		<div class="mb-3 flex h-10 w-10 items-center justify-center rounded-lg border border-line bg-raised text-fg-subtle">
			<Icon size={18} strokeWidth={1.75} />
		</div>
	{/if}
	<p class="text-sm font-medium text-fg">{title}</p>
	{#if description}<p class="mt-1 max-w-sm text-ui text-fg-subtle">{description}</p>{/if}
	{#if action}<div class="mt-4">{@render action()}</div>{/if}
</div>
