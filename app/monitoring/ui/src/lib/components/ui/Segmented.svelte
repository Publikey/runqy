<script lang="ts" generics="T extends string">
	// Filter switcher (one of N). Built on Skeleton's SegmentedControl (radio group).
	import { SegmentedControl } from '@skeletonlabs/skeleton-svelte';

	interface Item {
		value: T;
		label: string;
		count?: number;
		disabled?: boolean;
	}

	interface Props {
		items: Item[];
		value: T;
		onchange?: (value: T) => void;
		name?: string;
		label?: string;
	}

	let { items, value = $bindable(), onchange, name, label }: Props = $props();
</script>

<SegmentedControl
	{name}
	{value}
	onValueChange={(e) => {
		value = e.value as T;
		onchange?.(value);
	}}
	aria-label={label}
	class="max-w-full min-w-0"
>
	<SegmentedControl.Control class="rq-segmented">
		{#each items as item (item.value)}
			<SegmentedControl.Item value={item.value} disabled={item.disabled} class="rq-segmented-item">
				<SegmentedControl.ItemText>
					{item.label}{#if item.count !== undefined}<span class="rq-segmented-count">{item.count}</span>{/if}
				</SegmentedControl.ItemText>
				<SegmentedControl.ItemHiddenInput />
			</SegmentedControl.Item>
		{/each}
	</SegmentedControl.Control>
</SegmentedControl>
