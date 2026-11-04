<script lang="ts">
	import type { Component } from 'svelte';
	import { page } from '$app/stores';
	import { base } from '$app/paths';
	import { goto } from '$app/navigation';
	import {
		LayoutDashboard,
		Layers,
		Cpu,
		FlaskConical,
		TrendingUp,
		Bell,
		KeyRound,
		Server,
		Settings,
		LogOut,
		PanelLeftClose,
		PanelLeftOpen,
		ChevronsUpDown,
		ListChecks,
		Search
	} from '@lucide/svelte';
	import { paletteOpen } from '$lib/components/CommandPalette.svelte';
	import { Menu, Portal } from '@skeletonlabs/skeleton-svelte';
	import { settings } from '$lib/stores/settings';
	import { authStore } from '$lib/stores/auth';
	import { Logo, LiveIndicator } from '$lib/components/ui';

	let { mobileOpen = false }: { mobileOpen?: boolean } = $props();

	const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);

	async function handleLogout() {
		await authStore.logout();
		goto(`${base}/login`);
	}

	interface NavItem {
		label: string;
		href: string;
		icon: Component<any>;
	}

	const sections: { title: string; items: NavItem[] }[] = [
		{
			title: 'Operate',
			items: [
				{ label: 'Dashboard', href: '', icon: LayoutDashboard },
				{ label: 'Queues', href: '/queues', icon: Layers },
				{ label: 'Tasks', href: '/tasks', icon: ListChecks },
				{ label: 'Workers', href: '/workers', icon: Cpu },
				{ label: 'Playground', href: '/playground', icon: FlaskConical }
			]
		},
		{
			title: 'Scale',
			items: [
				{ label: 'Autoscaling', href: '/autoscaling', icon: TrendingUp },
				{ label: 'Alerts', href: '/alerts', icon: Bell }
			]
		},
		{
			title: 'Configure',
			items: [
				{ label: 'Vaults', href: '/vaults', icon: KeyRound },
				{ label: 'System', href: '/system', icon: Server },
				{ label: 'Settings', href: '/settings', icon: Settings }
			]
		}
	];

	let collapsed = $derived($settings.sidebarCollapsed);
	// Icon-only mode is a desktop feature; the mobile drawer always shows labels.
	let iconOnly = $derived(collapsed && !mobileOpen);

	function isActive(href: string): boolean {
		if (href === '') return $page.url.pathname === base || $page.url.pathname === base + '/';
		return $page.url.pathname.startsWith(base + href);
	}
</script>

<aside
	class="rq-sidebar h-screen flex flex-col transition-[width,transform] duration-200 ease-out
		fixed inset-y-0 left-0 z-40 w-[248px] {mobileOpen ? 'translate-x-0 shadow-2xl' : '-translate-x-full'}
		md:static md:translate-x-0 md:shadow-none {collapsed ? 'md:w-[60px]' : 'md:w-[232px]'}"
	style="padding-top: env(safe-area-inset-top);"
	aria-label="Main navigation"
>
	<!-- Brand + collapse -->
	<div class="flex h-14 shrink-0 items-center gap-2 {iconOnly ? 'justify-center px-2' : 'justify-between pl-4 pr-2.5'}">
		{#if !iconOnly}
			<a href="{base}/" class="rounded-md" aria-label="runqy dashboard">
				<Logo size={22} wordmark />
			</a>
		{/if}
		<button
			type="button"
			class="rq-btn rq-btn-icon rq-btn-sm rq-btn-v-ghost hidden md:inline-flex"
			onclick={() => settings.toggleSidebar()}
			aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
			title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
		>
			{#if collapsed}
				<PanelLeftOpen size={16} strokeWidth={1.75} />
			{:else}
				<PanelLeftClose size={16} strokeWidth={1.75} />
			{/if}
		</button>
	</div>

	<!-- Command palette -->
	<div class="px-2.5 pb-2">
		<button
			type="button"
			class="rq-nav-item w-full text-fg-subtle {iconOnly ? 'justify-center !px-0' : ''}"
			onclick={() => paletteOpen.set(true)}
			title={iconOnly ? 'Search (Ctrl+K)' : undefined}
			aria-label="Search (Ctrl+K)"
		>
			<Search size={16} strokeWidth={1.75} class="shrink-0" />
			{#if !iconOnly}
				<span class="flex-1 text-left">Search…</span>
				<kbd class="rq-kbd hidden md:inline-flex">{isMac ? '⌘' : 'Ctrl'} K</kbd>
			{/if}
		</button>
	</div>

	<!-- Navigation -->
	<nav class="flex-1 overflow-y-auto px-2.5 pb-4 pt-1">
		{#each sections as section, si (section.title)}
			<div class={si > 0 ? 'mt-5' : ''}>
				{#if iconOnly}
					{#if si > 0}<div class="mx-2 mb-3 border-t border-line-subtle"></div>{/if}
				{:else}
					<div class="px-2.5 pb-1.5 text-xs font-medium text-fg-subtle">{section.title}</div>
				{/if}
				<ul class="space-y-0.5">
					{#each section.items as item (item.href)}
						{@const active = isActive(item.href)}
						<li>
							<a
								href="{base}{item.href}"
								class="rq-nav-item {active ? 'rq-nav-item-active' : ''} {iconOnly ? 'justify-center !px-0' : ''}"
								aria-current={active ? 'page' : undefined}
								title={iconOnly ? item.label : undefined}
							>
								<item.icon size={16} strokeWidth={1.75} class="shrink-0" />
								{#if !iconOnly}<span class="truncate">{item.label}</span>{/if}
							</a>
						</li>
					{/each}
				</ul>
			</div>
		{/each}
	</nav>

	<!-- Freshness + account -->
	<div class="shrink-0 border-t border-line-subtle p-2.5 space-y-2">
		{#if !iconOnly}
			<div class="px-1"><LiveIndicator /></div>
		{:else}
			<div class="flex justify-center"><LiveIndicator compact /></div>
		{/if}
		{#if $authStore.email}
			<Menu
				positioning={{ placement: iconOnly ? 'right-end' : 'top-start', sameWidth: !iconOnly }}
				onSelect={(e) => {
					if (e.value === 'logout') handleLogout();
				}}
			>
				<Menu.Trigger
					class="rq-nav-item w-full {iconOnly ? 'justify-center !px-0' : ''}"
					title={iconOnly ? $authStore.email : undefined}
				>
					<span
						class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full border border-line-strong bg-raised text-2xs font-semibold text-fg"
					>
						{$authStore.email.charAt(0).toUpperCase()}
					</span>
					{#if !iconOnly}
						<span class="min-w-0 flex-1 text-left">
							<span class="block truncate text-ui font-medium text-fg">{$authStore.email}</span>
							<span class="block text-2xs text-fg-subtle">Admin</span>
						</span>
						<ChevronsUpDown size={14} strokeWidth={1.75} class="shrink-0 text-fg-subtle" />
					{/if}
				</Menu.Trigger>
				<Portal>
					<Menu.Positioner>
						<Menu.Content class="rq-menu">
							<div class="px-2 py-1.5 text-xs text-fg-subtle truncate">{$authStore.email}</div>
							<Menu.Separator class="rq-menu-separator" />
							<Menu.Item value="logout" class="rq-menu-item">
								<span class="flex items-center gap-2"><LogOut size={14} strokeWidth={1.75} />Sign out</span>
							</Menu.Item>
						</Menu.Content>
					</Menu.Positioner>
				</Portal>
			</Menu>
		{/if}
	</div>
</aside>
