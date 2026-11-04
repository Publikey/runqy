<script lang="ts">
	import { onMount } from 'svelte';
	import { browser } from '$app/environment';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { base } from '$app/paths';
	import '../app.css';
	import Sidebar from '$lib/components/Sidebar.svelte';
	import Toast from '$lib/components/Toast.svelte';
	import { Logo, Spinner } from '$lib/components/ui';
	import { Menu } from '@lucide/svelte';
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
	<div class="min-h-screen flex flex-col items-center justify-center gap-5 bg-canvas">
		<Logo size={36} />
		<Spinner size={18} class="text-fg-subtle" />
	</div>
{:else if showSidebar}
	<div class="flex flex-col md:flex-row h-screen overflow-hidden">
		<!-- Mobile topbar -->
		<header
			class="md:hidden shrink-0 flex items-center gap-2 h-12 px-2 bg-panel border-b border-line-subtle"
			style="padding-top: env(safe-area-inset-top); box-sizing: content-box;"
		>
			<button
				type="button"
				class="rq-btn rq-btn-icon rq-btn-v-ghost !w-11 !h-11"
				aria-label="Open navigation menu"
				onclick={() => (mobileNavOpen = true)}
			>
				<Menu size={18} strokeWidth={1.75} />
			</button>
			<a href="{base}/" aria-label="runqy dashboard"><Logo size={20} wordmark /></a>
		</header>

		<!-- Mobile drawer backdrop -->
		{#if mobileNavOpen}
			<!-- svelte-ignore a11y_click_events_have_key_events -->
			<!-- svelte-ignore a11y_no_static_element_interactions -->
			<div
				class="rq-dialog-backdrop fixed inset-0 z-30 md:hidden"
				data-state="open"
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
