<script lang="ts">
	import type { Snippet } from 'svelte';
	import { Dialog, Portal } from '@skeletonlabs/skeleton-svelte';
	import { X } from '@lucide/svelte';

	interface Props {
		open?: boolean;
		title: string;
		description?: string;
		size?: 'sm' | 'md' | 'lg';
		/** Buttons, right-aligned. */
		footer?: Snippet;
		/** Called whenever the dialog closes (Escape, backdrop, close button). */
		onclose?: () => void;
		/** alertdialog for confirmations: no close on outside click. */
		role?: 'dialog' | 'alertdialog';
		children?: Snippet;
	}

	let {
		open = $bindable(false),
		title,
		description,
		size = 'md',
		footer,
		onclose,
		role = 'dialog',
		children
	}: Props = $props();

	const widths = { sm: 'max-w-sm', md: 'max-w-lg', lg: 'max-w-2xl' };

	function handleOpenChange(details: { open: boolean }) {
		open = details.open;
		if (!details.open) onclose?.();
	}
</script>

<Dialog {open} onOpenChange={handleOpenChange} {role} closeOnInteractOutside={role === 'dialog'}>
	<Portal>
		<Dialog.Backdrop class="rq-dialog-backdrop fixed inset-0 z-50" />
		<Dialog.Positioner class="fixed inset-0 z-50 flex items-end sm:items-center justify-center p-0 sm:p-4">
			<Dialog.Content
				class="rq-dialog w-full {widths[size]} max-h-[92vh] sm:max-h-[85vh] flex flex-col rounded-t-xl sm:rounded-xl"
			>
				<header class="flex items-start justify-between gap-4 px-5 pt-5 pb-3">
					<div class="min-w-0">
						<Dialog.Title class="text-base font-semibold text-fg">{title}</Dialog.Title>
						{#if description}
							<Dialog.Description class="mt-1 text-ui text-fg-muted">{description}</Dialog.Description>
						{/if}
					</div>
					<Dialog.CloseTrigger class="rq-btn rq-btn-icon rq-btn-sm rq-btn-v-ghost -mr-1.5 -mt-1" aria-label="Close">
						<X size={16} strokeWidth={1.75} />
					</Dialog.CloseTrigger>
				</header>
				<div class="flex-1 overflow-y-auto px-5 pb-5">
					{@render children?.()}
				</div>
				{#if footer}
					<footer class="flex flex-wrap justify-end gap-2 px-5 py-3 border-t border-line-subtle bg-panel rounded-b-xl">
						{@render footer()}
					</footer>
				{/if}
			</Dialog.Content>
		</Dialog.Positioner>
	</Portal>
</Dialog>
