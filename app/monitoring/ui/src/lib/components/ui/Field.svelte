<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		label: string;
		/** id of the control, for the label's `for`. */
		for?: string;
		hint?: string;
		error?: string;
		required?: boolean;
		/** Optional right side of the label row (e.g. a link). */
		aside?: Snippet;
		class?: string;
		children: Snippet;
	}

	let { label, for: htmlFor, hint, error, required = false, aside, class: klass = '', children }: Props = $props();
</script>

<div class="space-y-1.5 {klass}">
	<div class="flex items-center justify-between gap-2">
		<label for={htmlFor} class="text-ui font-medium text-fg-muted">
			{label}{#if required}<span class="text-fg-faint ml-0.5">*</span>{/if}
		</label>
		{@render aside?.()}
	</div>
	{@render children()}
	{#if error}
		<p class="text-xs text-error-300">{error}</p>
	{:else if hint}
		<p class="text-xs text-fg-subtle">{hint}</p>
	{/if}
</div>
