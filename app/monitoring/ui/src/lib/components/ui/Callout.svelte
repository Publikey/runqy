<script lang="ts">
	// Inline message banner (load errors, warnings, notes).
	import type { Snippet } from 'svelte';
	import { CircleAlert, TriangleAlert, Info } from '@lucide/svelte';

	interface Props {
		tone?: 'danger' | 'warning' | 'info';
		title?: string;
		class?: string;
		children?: Snippet;
	}

	let { tone = 'danger', title, class: klass = '', children }: Props = $props();

	const styles = {
		danger: 'border-error-500/30 bg-error-500/10 text-error-200',
		warning: 'border-warning-500/30 bg-warning-500/10 text-warning-200',
		info: 'border-primary-500/30 bg-primary-500/10 text-primary-200'
	};
	const icons = { danger: CircleAlert, warning: TriangleAlert, info: Info };
	let Icon = $derived(icons[tone]);
</script>

<div class="flex items-start gap-2.5 rounded-md border px-3 py-2.5 text-ui {styles[tone]} {klass}" role={tone === 'info' ? 'status' : 'alert'}>
	<Icon size={16} strokeWidth={1.75} class="mt-0.5 shrink-0" />
	<div class="min-w-0 break-words">
		{#if title}<p class="font-medium">{title}</p>{/if}
		{@render children?.()}
	</div>
</div>
