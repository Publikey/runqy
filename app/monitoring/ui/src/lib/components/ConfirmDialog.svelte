<script lang="ts">
	import { Button, Modal } from '$lib/components/ui';

	interface Props {
		open?: boolean;
		title?: string;
		message?: string;
		confirmText?: string;
		cancelText?: string;
		variant?: 'default' | 'danger';
		onconfirm?: () => void;
		oncancel?: () => void;
	}

	let {
		open = $bindable(false),
		title = 'Confirm',
		message = 'Are you sure?',
		confirmText = 'Confirm',
		cancelText = 'Cancel',
		variant = 'default',
		onconfirm,
		oncancel
	}: Props = $props();

	// Set when the dialog closes through the confirm button, so onclose does not also
	// report a cancel.
	let confirmed = false;

	function handleConfirm() {
		confirmed = true;
		onconfirm?.();
		open = false;
	}

	function handleClose() {
		if (!confirmed) oncancel?.();
		confirmed = false;
	}
</script>

<Modal bind:open {title} size="sm" role="alertdialog" onclose={handleClose}>
	<p class="text-sm text-fg-muted">{message}</p>
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (open = false)}>{cancelText}</Button>
		<Button variant={variant === 'danger' ? 'danger' : 'primary'} onclick={handleConfirm}>{confirmText}</Button>
	{/snippet}
</Modal>
