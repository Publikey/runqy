<script lang="ts">
	// Frame of the signed-out screens (login, first-run setup).
	import type { Snippet } from 'svelte';
	import { Callout, Logo } from '$lib/components/ui';

	interface Props {
		title: string;
		subtitle: string;
		error?: string | null;
		footnote?: string;
		children: Snippet;
	}

	let { title, subtitle, error, footnote, children }: Props = $props();
</script>

<div class="relative min-h-screen flex items-center justify-center bg-canvas px-4 py-12 overflow-hidden">
	<!-- Faint top light, the only decoration of the signed-out screens. -->
	<div
		class="pointer-events-none absolute inset-x-0 top-0 h-[420px] opacity-60"
		style="background: radial-gradient(600px 300px at 50% -10%, rgb(76 130 247 / 0.14), transparent 70%);"
		aria-hidden="true"
	></div>

	<div class="relative w-full max-w-[400px] animate-fade-in">
		<div class="flex flex-col items-center text-center mb-8">
			<Logo size={40} />
			<h1 class="mt-5 text-xl font-semibold tracking-tight text-fg">{title}</h1>
			<p class="mt-1.5 text-sm text-fg-muted">{subtitle}</p>
		</div>

		<div class="rq-card p-6 sm:p-7">
			{#if error}
				<Callout class="mb-5">{error}</Callout>
			{/if}
			{@render children()}
		</div>

		{#if footnote}
			<p class="mt-6 text-center text-xs text-fg-subtle">{footnote}</p>
		{/if}
	</div>
</div>
