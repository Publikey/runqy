<script lang="ts">
	import type { Component, Snippet } from 'svelte';
	import type { HTMLButtonAttributes } from 'svelte/elements';
	import Spinner from './Spinner.svelte';

	type Variant = 'primary' | 'secondary' | 'ghost' | 'danger';

	interface Props extends Omit<HTMLButtonAttributes, 'children'> {
		variant?: Variant;
		size?: 'sm' | 'md';
		/** Lucide icon component shown before the label. */
		icon?: Component<any>;
		loading?: boolean;
		/** Render as a link. */
		href?: string;
		children?: Snippet;
	}

	let {
		variant = 'secondary',
		size = 'md',
		icon: Icon,
		loading = false,
		href,
		disabled,
		type = 'button',
		class: klass = '',
		children,
		...rest
	}: Props = $props();

	let classes = $derived(
		`rq-btn rq-btn-v-${variant} ${size === 'sm' ? 'rq-btn-sm' : ''} ${klass}`
	);
	let iconSize = $derived(size === 'sm' ? 14 : 16);
</script>

{#if href}
	<a {href} class={classes} aria-disabled={disabled || loading ? 'true' : undefined}>
		{#if Icon}<Icon size={iconSize} strokeWidth={1.75} />{/if}
		{@render children?.()}
	</a>
{:else}
	<button {type} class={classes} disabled={disabled || loading} aria-busy={loading || undefined} {...rest}>
		{#if loading}
			<Spinner size={iconSize} />
		{:else if Icon}
			<Icon size={iconSize} strokeWidth={1.75} />
		{/if}
		{@render children?.()}
	</button>
{/if}
