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
	<div class="flex h-screen overflow-hidden">
		<Sidebar />

		<main class="main-content flex-1 overflow-y-auto">
			{@render children()}
		</main>
	</div>
{:else}
	{@render children()}
{/if}

<Toast />
