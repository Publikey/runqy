<script lang="ts" module>
	import type { Snippet } from 'svelte';

	export interface KV {
		label: string;
		value?: string | number | null;
		mono?: boolean;
		/** Custom rendering of the value. */
		render?: Snippet;
	}
</script>

<script lang="ts">
	interface Props {
		items: KV[];
		/** Columns from md up. */
		columns?: 1 | 2 | 3;
		/** Label left, value right on one line (compact lists). */
		inline?: boolean;
	}

	let { items, columns = 2, inline = false }: Props = $props();

	const cols = { 1: 'md:grid-cols-1', 2: 'md:grid-cols-2', 3: 'md:grid-cols-3' };
</script>

{#if inline}
	<dl class="divide-y divide-line-subtle">
		{#each items as item (item.label)}
			<div class="flex items-center justify-between gap-4 py-2 first:pt-0 last:pb-0">
				<dt class="text-ui text-fg-muted">{item.label}</dt>
				<dd class="text-ui text-fg text-right min-w-0 truncate {item.mono ? 'font-mono' : ''}">
					{#if item.render}{@render item.render()}{:else}{item.value ?? '—'}{/if}
				</dd>
			</div>
		{/each}
	</dl>
{:else}
	<dl class="grid grid-cols-1 {cols[columns]} gap-x-8 gap-y-4">
		{#each items as item (item.label)}
			<div class="min-w-0">
				<dt class="text-xs text-fg-subtle">{item.label}</dt>
				<dd class="mt-0.5 text-ui text-fg break-words {item.mono ? 'font-mono' : ''}">
					{#if item.render}{@render item.render()}{:else}{item.value ?? '—'}{/if}
				</dd>
			</div>
		{/each}
	</dl>
{/if}
