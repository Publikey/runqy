<script lang="ts">
	let { size = 16, class: klass = '' }: { size?: number; class?: string } = $props();
</script>

<svg
	class="rq-spin shrink-0 {klass}"
	width={size}
	height={size}
	viewBox="0 0 24 24"
	fill="none"
	role="status"
	aria-label="Loading"
>
	<circle cx="12" cy="12" r="9" stroke="currentColor" stroke-opacity="0.2" stroke-width="2.5" />
	<path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" />
</svg>
