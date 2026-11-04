<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		title?: string;
		description?: string;
		/** Right side of the header (buttons, links, filters). */
		actions?: Snippet;
		/** Extra content next to the title (badges). */
		titleExtra?: Snippet;
		footer?: Snippet;
		/** No body padding: for tables and lists that run edge to edge. */
		flush?: boolean;
		class?: string;
		bodyClass?: string;
		children?: Snippet;
	}

	let {
		title,
		description,
		actions,
		titleExtra,
		footer,
		flush = false,
		class: klass = '',
		bodyClass = '',
		children
	}: Props = $props();

	let hasHeader = $derived(Boolean(title || description || actions || titleExtra));
</script>

<section class="rq-card {klass}">
	{#if hasHeader}
		<header
			class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 px-3.5 md:px-5 py-2.5 md:py-3 border-b border-line-subtle"
		>
			<div class="min-w-0">
				{#if title}
					<div class="flex flex-wrap items-center gap-2">
						<h2 class="text-sm font-semibold text-fg">{title}</h2>
						{@render titleExtra?.()}
					</div>
				{/if}
				{#if description}
					<p class="text-ui text-fg-subtle mt-0.5">{description}</p>
				{/if}
			</div>
			{#if actions}
				<div class="flex flex-wrap items-center gap-2">{@render actions()}</div>
			{/if}
		</header>
	{/if}
	<div class="{flush ? '' : 'p-3.5 md:p-5'} {bodyClass}">
		{@render children?.()}
	</div>
	{#if footer}
		<footer class="px-3.5 md:px-5 py-2.5 md:py-3 border-t border-line-subtle">{@render footer()}</footer>
	{/if}
</section>
