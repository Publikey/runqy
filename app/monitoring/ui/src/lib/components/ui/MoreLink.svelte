<script lang="ts">
	// The one style of "go somewhere" link (View all →, Autoscaling →). Not a button.
	import { base } from '$app/paths';
	import { ArrowRight } from '@lucide/svelte';

	let { href, label }: { href: string; label: string } = $props();
</script>

<a href="{base}{href}" class="rq-more-link">{label}<ArrowRight size={14} strokeWidth={1.75} /></a>
