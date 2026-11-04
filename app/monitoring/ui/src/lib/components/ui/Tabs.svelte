<script lang="ts" generics="T extends string">
	// Underlined section tabs (content is rendered by the caller from `value`).
	// Built on Skeleton's Tabs for keyboard navigation and ARIA.
	import { Tabs } from '@skeletonlabs/skeleton-svelte';
	import { formatNumber } from '$lib/utils/format';

	interface Item {
		value: T;
		label: string;
		count?: number;
	}

	interface Props {
		items: Item[];
		value: T;
		onchange?: (value: T) => void;
	}

	let { items, value = $bindable(), onchange }: Props = $props();
</script>

<Tabs
	{value}
	onValueChange={(e) => {
		value = e.value as T;
		onchange?.(value);
	}}
>
	<Tabs.List class="rq-tabs">
		{#each items as item (item.value)}
			<Tabs.Trigger value={item.value} class="rq-tab">
				{item.label}
				{#if item.count !== undefined}<span class="rq-tab-count">{formatNumber(item.count)}</span>{/if}
			</Tabs.Trigger>
		{/each}
		<Tabs.Indicator class="rq-tabs-indicator" />
	</Tabs.List>
</Tabs>
