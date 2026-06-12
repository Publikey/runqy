<script lang="ts">
	import { onMount } from 'svelte';
	import { browser } from '$app/environment';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { base } from '$app/paths';
	import '../app.css';
	import Sidebar from '$lib/components/Sidebar.svelte';
	import Toast from '$lib/components/Toast.svelte';
	import { authStore } from '$lib/stores/auth';

	let { children } = $props();

	let mobileNavOpen = $state(false);

	// Close the mobile drawer on every navigation.
	$effect(() => {
		void $page.url.pathname;
		mobileNavOpen = false;
	});

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') mobileNavOpen = false;
	}

	// Public routes that don't require authentication
	const publicRoutes = ['/login', '/setup'];

	function isPublicRoute(pathname: string): boolean {
		const path = pathname.replace(base, '');
		return publicRoutes.some((route) => path === route || path.startsWith(route + '/'));
	}

	// Auth guard effect
	$effect(() => {
		if (!browser || !$authStore.checked) return;

		const pathname = $page.url.pathname;
		const isPublic = isPublicRoute(pathname);

		if ($authStore.setupRequired) {
			// Redirect to setup if admin needs to be created
			if (!pathname.endsWith('/setup')) {
				goto(`${base}/setup`);
			}
		} else if (!$authStore.authenticated && !isPublic) {
			// Redirect to login if not authenticated
			goto(`${base}/login`);
		} else if ($authStore.authenticated && isPublic) {
			// Redirect to home if authenticated and on public page
			goto(base || '/');
		}
	});

	onMount(() => {
		if (!browser) return;

		// Check auth status on mount
		authStore.checkStatus();

		// Listen for auth:unauthorized events from API client
		const handleUnauthorized = () => {
			authStore.checkStatus();
		};
		window.addEventListener('auth:unauthorized', handleUnauthorized);

		return () => {
			window.removeEventListener('auth:unauthorized', handleUnauthorized);
		};
	});

	let showSidebar = $derived(!isPublicRoute($page.url.pathname));
	let showLoading = $derived(!$authStore.checked);
</script>

{#if showLoading}
	<div class="min-h-screen flex items-center justify-center bg-[#0a0a0f]">
		<div class="text-center">
			<div class="animate-spin mb-4">
				<svg class="w-8 h-8 mx-auto text-primary-500" fill="none" viewBox="0 0 24 24">
					<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
					<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
				</svg>
			</div>
			<p class="text-surface-500">Loading...</p>
		</div>
	</div>
{:else if showSidebar}
	<div class="flex flex-col md:flex-row h-screen overflow-hidden">
		<!-- Mobile topbar -->
		<header
			class="md:hidden shrink-0 flex items-center gap-3 px-4 py-2.5 bg-surface-800"
			style="border-bottom: 1px solid var(--runqy-border-subtle); padding-top: calc(0.625rem + env(safe-area-inset-top));"
		>
			<button
				type="button"
				class="rq-btn-ghost !px-2.5"
				aria-label="Open navigation menu"
				onclick={() => (mobileNavOpen = true)}
			>
				<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
				</svg>
			</button>
			<svg viewBox="0 0 100 100" class="w-6 h-6">
				<line x1="32" y1="50" x2="72" y2="22" stroke="#64748B" stroke-width="5" stroke-linecap="round" />
				<line x1="32" y1="50" x2="78" y2="50" stroke="#64748B" stroke-width="5" stroke-linecap="round" />
				<line x1="32" y1="50" x2="72" y2="78" stroke="#64748B" stroke-width="5" stroke-linecap="round" />
				<circle cx="32" cy="50" r="16" fill="#3B82F6" />
				<circle cx="72" cy="22" r="11" fill="#E2E8F0" />
				<circle cx="78" cy="50" r="11" fill="#E2E8F0" />
				<circle cx="72" cy="78" r="11" fill="#E2E8F0" />
			</svg>
			<span class="font-bold">runqy <span class="text-[#3DCFBC] font-normal">Monitor</span></span>
		</header>

		<!-- Mobile drawer backdrop -->
		{#if mobileNavOpen}
			<!-- svelte-ignore a11y_click_events_have_key_events -->
			<!-- svelte-ignore a11y_no_static_element_interactions -->
			<div
				class="fixed inset-0 z-30 bg-surface-950/60 md:hidden"
				onclick={() => (mobileNavOpen = false)}
			></div>
		{/if}

		<Sidebar mobileOpen={mobileNavOpen} />

		<main class="main-content flex-1 overflow-y-auto">
			{@render children()}
		</main>
	</div>
{:else}
	{@render children()}
{/if}

<svelte:window onkeydown={handleKeydown} />

<Toast />
