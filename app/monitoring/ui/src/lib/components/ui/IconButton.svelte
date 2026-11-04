<script lang="ts">
	import type { Component } from 'svelte';
	import { Tooltip, Portal } from '@skeletonlabs/skeleton-svelte';
	import Spinner from './Spinner.svelte';

	interface Props {
		icon: Component<any>;
		/** Accessible name, also shown as the tooltip. */
		label: string;
		variant?: 'ghost' | 'secondary';
		size?: 'sm' | 'md';
		/** Neutral until hovered, then red: for row-level deletes. */
		danger?: boolean;
		loading?: boolean;
		disabled?: boolean;
		onclick?: (e: MouseEvent) => void;
		class?: string;
	}

	let {
		icon: Icon,
		label,
		variant = 'ghost',
		size = 'sm',
		danger = false,
		loading = false,
		disabled = false,
		onclick,
		class: klass = ''
	}: Props = $props();

	let iconSize = $derived(size === 'sm' ? 15 : 16);
</script>

<Tooltip openDelay={400} closeDelay={0} positioning={{ placement: 'top' }}>
	<Tooltip.Trigger>
		{#snippet element(attributes)}
			<button
				{...attributes}
				type="button"
				aria-label={label}
				class="rq-btn rq-btn-icon rq-btn-v-{variant} {size === 'sm' ? 'rq-btn-sm' : ''} {danger
					? 'rq-btn-danger-hover'
					: ''} {klass}"
				disabled={disabled || loading}
				onclick={(e) => {
					e.stopPropagation();
					onclick?.(e);
				}}
			>
				{#if loading}
					<Spinner size={iconSize} />
				{:else}
					<Icon size={iconSize} strokeWidth={1.75} />
				{/if}
			</button>
		{/snippet}
	</Tooltip.Trigger>
	<Portal>
		<Tooltip.Positioner>
			<Tooltip.Content class="rq-tooltip">{label}</Tooltip.Content>
		</Tooltip.Positioner>
	</Portal>
</Tooltip>
