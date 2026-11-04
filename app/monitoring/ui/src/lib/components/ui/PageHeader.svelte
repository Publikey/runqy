<script lang="ts">
	import type { Snippet } from 'svelte';
	import { base } from '$app/paths';
	import { ChevronRight } from '@lucide/svelte';
	import { refreshRequests } from '$lib/stores/live';

	interface Crumb {
		label: string;
		/** App-relative path, e.g. '/queues'. */
		href?: string;
	}

	interface Props {
		title: string;
		subtitle?: string;
		breadcrumbs?: Crumb[];
		/** Badges or meta next to the title. */
		titleExtra?: Snippet;
		/** Primary actions, right-aligned. */
		actions?: Snippet;
		/** Deprecated: freshness is shown by the global Live indicator. Ignored. */
		updatedAt?: Date | null;
		/** Reloads the page data when the user clicks the Live indicator. */
		onrefresh?: () => void | Promise<void>;
		/** Monospace title, for technical ids (task ids). */
		mono?: boolean;
	}

	let { title, subtitle, breadcrumbs, titleExtra, actions, onrefresh, mono = false }: Props = $props();

	// The Live indicator (sidebar / mobile bar) asks for a refresh; the page reloads its data.
	$effect(() => {
		let first = true;
		return refreshRequests.subscribe(() => {
			if (first) {
				first = false;
				return;
			}
			onrefresh?.();
		});
	});
</script>

<header class="mb-4 md:mb-6">
	{#if breadcrumbs?.length}
		<nav aria-label="Breadcrumb" class="flex items-center gap-1 text-ui text-fg-subtle mb-2">
			{#each breadcrumbs as crumb, i (i)}
				{#if i > 0}<ChevronRight size={14} strokeWidth={1.75} class="text-fg-faint" />{/if}
				{#if crumb.href}
					<a href="{base}{crumb.href}" class="hover:text-fg transition-colors">{crumb.label}</a>
				{:else}
					<span class="text-fg-muted">{crumb.label}</span>
				{/if}
			{/each}
		</nav>
	{/if}
	<div class="flex flex-wrap items-start justify-between gap-x-4 gap-y-3">
		<!-- Basis 12rem: the actions wrap below the title before the title gets squeezed. -->
		<div class="min-w-0 flex-[1_1_12rem]">
			<div class="flex flex-wrap items-center gap-x-3 gap-y-1">
				<h1 class="rq-page-title truncate {mono ? 'font-mono' : ''}">{title}</h1>
				{@render titleExtra?.()}
			</div>
			{#if subtitle}
				<p class="rq-page-subtitle">{subtitle}</p>
			{/if}
		</div>
		{#if actions}
			<div class="flex flex-wrap items-center gap-2 max-w-full">
				{@render actions()}
			</div>
		{/if}
	</div>
</header>
