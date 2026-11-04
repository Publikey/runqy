<script lang="ts">
	import { Toast } from '@skeletonlabs/skeleton-svelte';
	import { CircleCheck, CircleAlert, TriangleAlert, Info, X } from '@lucide/svelte';
	import { toaster } from '$lib/stores/toaster';

	const icons = { success: CircleCheck, error: CircleAlert, warning: TriangleAlert, info: Info };
	const tones = {
		success: 'text-success-400',
		error: 'text-error-400',
		warning: 'text-warning-400',
		info: 'text-primary-400'
	};
</script>

<Toast.Group {toaster}>
	{#snippet children(toast)}
		{@const kind = (toast.type ?? 'info') as keyof typeof icons}
		{@const Icon = icons[kind] ?? Info}
		<Toast {toast}>
			<Icon size={16} strokeWidth={1.75} class="shrink-0 self-start mt-0.5 {tones[kind] ?? tones.info}" />
			<Toast.Message>
				<Toast.Title>{toast.title}</Toast.Title>
				{#if toast.description}<Toast.Description>{toast.description}</Toast.Description>{/if}
			</Toast.Message>
			<Toast.CloseTrigger class="rq-btn rq-btn-icon rq-btn-sm rq-btn-v-ghost self-start -mr-1 -mt-1" aria-label="Dismiss">
				<X size={14} strokeWidth={1.75} />
			</Toast.CloseTrigger>
		</Toast>
	{/snippet}
</Toast.Group>
