<script lang="ts">
	import type { Snippet } from 'svelte';
	import { base } from '$app/paths';
	import { ChevronRight, RefreshCw } from '@lucide/svelte';
	import { formatRelativeTime } from '$lib/utils/format';
	import IconButton from './IconButton.svelte';

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
		/** Shows "Updated …" and a refresh button when set. */
		updatedAt?: Date | null;
		onrefresh?: () => void | Promise<void>;
		/** Monospace title (ids, queue names). */
		mono?: boolean;
	}

	let { title, subtitle, breadcrumbs, titleExtra, actions, updatedAt, onrefresh, mono = false }: Props =
		$props();

	let refreshing = $state(false);
	// Re-render the relative time every 15 s so "Updated 1m ago" stays truthful.
	let tick = $state(0);
	$effect(() => {
		const t = setInterval(() => (tick += 1), 15000);
		return () => clearInterval(t);
	});
	let updatedLabel = $derived.by(() => {
		void tick;
		return updatedAt ? `Updated ${formatRelativeTime(updatedAt)}` : '';
	});

	async function refresh() {
		if (!onrefresh || refreshing) return;
		refreshing = true;
		try {
			await onrefresh();
		} finally {
			setTimeout(() => (refreshing = false), 300);
		}
	}
</script>

<header class="mb-6">
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
		<div class="min-w-0 flex-1">
			<div class="flex flex-wrap items-center gap-x-3 gap-y-1">
				<h1 class="rq-page-title truncate {mono ? 'font-mono' : ''}">{title}</h1>
				{@render titleExtra?.()}
			</div>
			{#if subtitle || updatedLabel}
				<p class="rq-page-subtitle">
					{subtitle ?? ''}{#if subtitle && updatedLabel}<span class="text-fg-faint mx-1.5">·</span>{/if}{#if updatedLabel}<span
							class="text-fg-subtle">{updatedLabel}</span
						>{/if}
				</p>
			{/if}
		</div>
		{#if actions || onrefresh}
			<div class="flex flex-wrap items-center gap-2 shrink-0">
				{#if onrefresh}
					<IconButton icon={RefreshCw} label="Refresh" variant="secondary" size="md" loading={refreshing} onclick={refresh} />
				{/if}
				{@render actions?.()}
			</div>
		{/if}
	</div>
</header>
