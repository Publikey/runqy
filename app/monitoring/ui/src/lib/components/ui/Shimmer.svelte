<script lang="ts">
	// Loading placeholder: `lines` shimmering bars, or one block sized by `class`.
	let { lines = 0, class: klass = 'h-4 w-full' }: { lines?: number; class?: string } = $props();
</script>

{#if lines > 0}
	<div class="space-y-3" aria-hidden="true">
		{#each Array.from({ length: lines }, (_, i) => i) as i (i)}
			<div class="rq-skeleton h-4" style="width: {i === lines - 1 ? 60 : 100}%"></div>
		{/each}
	</div>
{:else}
	<div class="rq-skeleton {klass}" aria-hidden="true"></div>
{/if}
