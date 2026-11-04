<script lang="ts">
	// runqy mark: one dispatcher node fanning out to three workers.
	let { size = 24, wordmark = false }: { size?: number; wordmark?: boolean } = $props();
</script>

<span class="inline-flex items-center gap-2.5 select-none">
	<svg viewBox="0 0 100 100" width={size} height={size} class="shrink-0" aria-hidden={wordmark ? 'true' : undefined} role={wordmark ? undefined : 'img'} aria-label={wordmark ? undefined : 'runqy'}>
		<g stroke="currentColor" class="text-fg-faint" stroke-width="6" stroke-linecap="round">
			<line x1="32" y1="50" x2="72" y2="22" />
			<line x1="32" y1="50" x2="78" y2="50" />
			<line x1="32" y1="50" x2="72" y2="78" />
		</g>
		<circle cx="32" cy="50" r="16" class="fill-primary-500" />
		<circle cx="72" cy="22" r="11" class="fill-fg" />
		<circle cx="78" cy="50" r="11" class="fill-fg" />
		<circle cx="72" cy="78" r="11" class="fill-fg" />
	</svg>
	{#if wordmark}
		<span class="text-[0.9375rem] font-semibold tracking-tight text-fg">runqy</span>
	{/if}
</span>
