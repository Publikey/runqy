<script lang="ts">
	import { page } from '$app/stores';
	import { base } from '$app/paths';
	import { goto } from '$app/navigation';
	import { settings } from '$lib/stores/settings';
	import { authStore } from '$lib/stores/auth';

	let { mobileOpen = false }: { mobileOpen?: boolean } = $props();

	async function handleLogout() {
		await authStore.logout();
		goto(`${base}/login`);
	}

	const navItems = [
		{
			label: 'Dashboard',
			href: '',
			icon: `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6"/>
			</svg>`,
			group: 'main'
		},
		{
			label: 'Queues',
			href: '/queues',
			icon: `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10"/>
			</svg>`,
			group: 'core'
		},
		{
			label: 'Workers',
			href: '/workers',
			icon: `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 3v2m6-2v2M9 19v2m6-2v2M5 9H3m2 6H3m18-6h-2m2 6h-2M7 19h10a2 2 0 002-2V7a2 2 0 00-2-2H7a2 2 0 00-2 2v10a2 2 0 002 2zM9 9h6v6H9V9z"/>
			</svg>`,
			group: 'core'
		},
		{
			label: 'Playground',
			href: '/playground',
			icon: `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z"/>
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/>
			</svg>`,
			group: 'core'
		},
		{
			label: 'Vaults',
			href: '/vaults',
			icon: `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z"/>
			</svg>`,
			group: 'core'
		},
		{
			label: 'System',
			href: '/system',
			icon: `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01"/>
			</svg>`,
			group: 'infra'
		},
		{
			label: 'Settings',
			href: '/settings',
			icon: `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"/>
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"/>
			</svg>`,
			group: 'infra'
		}
	];

	let collapsed = $derived($settings.sidebarCollapsed);
	// Icon-only mode is a desktop feature; the mobile drawer always shows labels.
	let iconOnly = $derived(collapsed && !mobileOpen);

	function isActive(href: string): boolean {
		const fullPath = base + href;
		if (href === '') return $page.url.pathname === base || $page.url.pathname === base + '/';
		return $page.url.pathname.startsWith(fullPath);
	}

	// Group items with dividers
	let groupedNav = $derived.by(() => {
		const groups: { items: typeof navItems; key: string }[] = [];
		let currentGroup: typeof navItems = [];
		let currentKey = navItems[0]?.group;

		for (const item of navItems) {
			if (item.group !== currentKey) {
				groups.push({ items: currentGroup, key: currentKey! });
				currentGroup = [];
				currentKey = item.group;
			}
			currentGroup.push(item);
		}
		if (currentGroup.length > 0) {
			groups.push({ items: currentGroup, key: currentKey! });
		}
		return groups;
	});
</script>

<aside
	class="rq-sidebar h-screen flex flex-col transition-[width,transform] duration-200 ease-in-out
		fixed inset-y-0 left-0 z-40 w-[240px] {mobileOpen ? 'translate-x-0' : '-translate-x-full'}
		md:static md:translate-x-0 {collapsed ? 'md:w-20' : 'md:w-[240px]'}"
	style="padding-top: env(safe-area-inset-top);"
>
	<!-- Logo / Header -->
	<div class="p-5 mt-2 ml-2" style="border-bottom: 1px solid var(--runqy-border-subtle);">
		<div class="flex items-center gap-3 {iconOnly ? 'justify-center' : ''}">
			<!-- Logo SVG -->
			<svg viewBox="0 0 100 100" class="w-8 h-8 flex-shrink-0">
				<line x1="32" y1="50" x2="72" y2="22" stroke="#64748B" stroke-width="5" stroke-linecap="round" />
				<line x1="32" y1="50" x2="78" y2="50" stroke="#64748B" stroke-width="5" stroke-linecap="round" />
				<line x1="32" y1="50" x2="72" y2="78" stroke="#64748B" stroke-width="5" stroke-linecap="round" />
				<circle cx="32" cy="50" r="16" fill="#3B82F6" />
				<circle cx="72" cy="22" r="11" fill="#E2E8F0" />
				<circle cx="78" cy="50" r="11" fill="#E2E8F0" />
				<circle cx="72" cy="78" r="11" fill="#E2E8F0" />
			</svg>
			{#if !iconOnly}
				<div>
					<span class="font-bold text-2xl">runqy</span>
					<span class="text-base text-[#3DCFBC] block">Monitor</span>
				</div>
			{/if}
		</div>
	</div>

	<!-- Navigation -->
	<nav class="flex-1 p-4 overflow-y-auto">
		{#each groupedNav as group, gi}
			{#if gi > 0}
				<div class="my-3 mx-3" style="border-top: 1px solid var(--runqy-border-subtle);"></div>
			{/if}
			<div class="space-y-1">
				{#each group.items as item}
					<a
						href="{base}{item.href}"
						class="{isActive(item.href) ? 'rq-sidebar-nav-item-active' : 'rq-sidebar-nav-item'} {iconOnly ? 'justify-center' : ''}"
						title={iconOnly ? item.label : undefined}
					>
						<span class="[&>svg]:w-5 [&>svg]:h-5 flex-shrink-0">{@html item.icon}</span>
						{#if !iconOnly}
							<span>{item.label}</span>
						{/if}
					</a>
				{/each}
			</div>
		{/each}
	</nav>

	<!-- User section -->
	{#if $authStore.email}
		<div class="p-3" style="border-top: 1px solid var(--runqy-border-subtle);">
			<div class="flex items-center gap-3 px-4 py-2 {iconOnly ? 'justify-center' : ''}">
				<div class="w-8 h-8 rounded-full bg-primary-500 flex items-center justify-center text-white text-sm font-medium flex-shrink-0">
					{$authStore.email.charAt(0).toUpperCase()}
				</div>
				{#if !iconOnly}
					<div class="flex-1 min-w-0">
						<p class="text-sm font-medium truncate">{$authStore.email}</p>
						<p class="text-xs text-surface-500">Admin</p>
					</div>
				{/if}
			</div>
			<button
				type="button"
				class="rq-sidebar-nav-item w-full {iconOnly ? 'justify-center' : ''}"
				onclick={handleLogout}
				title={iconOnly ? 'Logout' : undefined}
			>
				<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1"
					/>
				</svg>
				{#if !iconOnly}
					<span class="text-sm">Logout</span>
				{/if}
			</button>
		</div>
	{/if}

	<!-- Collapse toggle -->
	<div class="p-3" style="border-top: 1px solid var(--runqy-border-subtle);">
		<button
			type="button"
			class="rq-sidebar-nav-item w-full {iconOnly ? 'justify-center' : ''}"
			onclick={() => settings.toggleSidebar()}
		>
			<svg
				class="w-5 h-5 transition-transform {collapsed ? 'rotate-180' : ''}"
				fill="none"
				stroke="currentColor"
				viewBox="0 0 24 24"
			>
				<path
					stroke-linecap="round"
					stroke-linejoin="round"
					stroke-width="2"
					d="M11 19l-7-7 7-7m8 14l-7-7 7-7"
				/>
			</svg>
			{#if !iconOnly}
				<span class="text-sm">Collapse</span>
			{/if}
		</button>
	</div>
</aside>
