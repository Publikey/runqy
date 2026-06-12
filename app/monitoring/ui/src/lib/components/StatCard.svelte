<script lang="ts">
	import { base } from '$app/paths';
	import { formatNumber } from '$lib/utils/format';

	type Variant = 'default' | 'success' | 'warning' | 'error' | 'primary';

	interface Props {
		label: string;
		value: number;
		variant?: Variant;
		icon?: string;
		href?: string;
	}

	let { label, value, variant = 'default', icon, href }: Props = $props();

	const fullHref = href ? base + href : undefined;

	const variantClasses: Record<Variant, string> = {
		default: 'text-surface-100',
		success: 'text-success-500',
		warning: 'text-warning-500',
		error: 'text-error-500',
		primary: 'text-primary-500'
	};
</script>

{#if fullHref}
<a href={fullHref} class="rq-card rq-card-interactive p-5 block">
	<div class="flex items-start justify-between">
		<div>
			<p class="text-xs font-medium uppercase tracking-wider text-surface-400">{label}</p>
			<p class="text-2xl font-semibold tracking-tight mt-1 {variantClasses[variant]}">{formatNumber(value)}</p>
		</div>
		{#if icon}
			<div class="p-2 rounded-lg" style="background: rgba(255,255,255,0.04);">
				{@html icon}
			</div>
		{/if}
	</div>
</a>
{:else}
<div class="rq-card p-5">
	<div class="flex items-start justify-between">
		<div>
			<p class="text-xs font-medium uppercase tracking-wider text-surface-400">{label}</p>
			<p class="text-2xl font-semibold tracking-tight mt-1 {variantClasses[variant]}">{formatNumber(value)}</p>
		</div>
		{#if icon}
			<div class="p-2 rounded-lg" style="background: rgba(255,255,255,0.04);">
				{@html icon}
			</div>
		{/if}
	</div>
</div>
{/if}
