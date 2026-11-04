<script lang="ts">
	import type { VaultSummary } from '$lib/api/types';
	import { ChevronRight, KeyRound, Trash2 } from '@lucide/svelte';
	import { Badge, IconButton } from '$lib/components/ui';

	interface Props {
		vault: VaultSummary;
		onClick?: () => void;
		onDelete?: () => void;
	}

	let { vault, onClick, onDelete }: Props = $props();
</script>

<div
	class="rq-card rq-card-interactive p-4 flex flex-col gap-3 cursor-pointer"
	onclick={onClick}
	onkeydown={(e) => e.key === 'Enter' && onClick?.()}
	role="button"
	tabindex="0"
>
	<div class="flex items-start justify-between gap-2">
		<div class="flex items-center gap-2 min-w-0">
			<KeyRound size={16} strokeWidth={1.75} class="shrink-0 text-fg-subtle" />
			<h3 class="text-sm font-semibold text-fg font-mono truncate">{vault.name}</h3>
		</div>
		<IconButton icon={Trash2} label="Delete vault" danger onclick={() => onDelete?.()} class="-mr-1 -mt-1" />
	</div>

	{#if vault.description}
		<p class="text-ui text-fg-subtle line-clamp-2">{vault.description}</p>
	{/if}

	<div class="mt-auto flex items-center justify-between">
		<Badge tone="outline">{vault.entry_count} {vault.entry_count === 1 ? 'entry' : 'entries'}</Badge>
		<ChevronRight size={16} strokeWidth={1.75} class="text-fg-faint" />
	</div>
</div>
