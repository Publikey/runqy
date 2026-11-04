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
		PanelLeftOpen
	} from '@lucide/svelte';
	import { settings } from '$lib/stores/settings';
	import { authStore } from '$lib/stores/auth';
	import { Logo } from '$lib/components/ui';

	let { mobileOpen = false }: { mobileOpen?: boolean } = $props();

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
	<!-- Brand -->
	<div class="flex h-14 shrink-0 items-center {iconOnly ? 'justify-center px-2' : 'px-4'}">
		<a href="{base}/" class="rounded-md" aria-label="runqy dashboard">
			<Logo size={22} wordmark={!iconOnly} />
		</a>
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

	<!-- Account + collapse -->
	<div class="shrink-0 border-t border-line-subtle p-2.5 space-y-0.5">
		{#if $authStore.email}
			<div class="flex items-center gap-2.5 rounded-md px-2.5 py-1.5 {iconOnly ? 'justify-center !px-0' : ''}" title={$authStore.email}>
				<div class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full border border-line-strong bg-raised text-[0.6875rem] font-semibold text-fg">
					{$authStore.email.charAt(0).toUpperCase()}
				</div>
				{#if !iconOnly}
					<div class="min-w-0 flex-1">
						<p class="truncate text-ui font-medium text-fg">{$authStore.email}</p>
						<p class="text-2xs text-fg-subtle">Admin</p>
					</div>
				{/if}
			</div>
			<button
				type="button"
				class="rq-nav-item w-full {iconOnly ? 'justify-center !px-0' : ''}"
				onclick={handleLogout}
				title={iconOnly ? 'Sign out' : undefined}
			>
				<LogOut size={16} strokeWidth={1.75} class="shrink-0" />
				{#if !iconOnly}<span>Sign out</span>{/if}
			</button>
		{/if}
		<button
			type="button"
			class="rq-nav-item w-full hidden md:flex {iconOnly ? 'justify-center !px-0' : ''}"
			onclick={() => settings.toggleSidebar()}
			title={iconOnly ? 'Expand sidebar' : undefined}
		>
			{#if collapsed}
				<PanelLeftOpen size={16} strokeWidth={1.75} class="shrink-0" />
			{:else}
				<PanelLeftClose size={16} strokeWidth={1.75} class="shrink-0" />
				<span>Collapse</span>
			{/if}
		</button>
	</div>
</aside>
