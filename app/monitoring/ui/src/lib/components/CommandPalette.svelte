<script lang="ts" module>
	import { writable } from 'svelte/store';

	/** Open state of the command palette (⌘K / Ctrl+K, or the sidebar button). */
	export const paletteOpen = writable(false);
</script>

<script lang="ts">
	// Command palette: jump to a page, queue, worker or task id, or run a quick action.
	import type { Component } from 'svelte';
	import { tick } from 'svelte';
	import { Dialog, Portal } from '@skeletonlabs/skeleton-svelte';
	import { goto } from '$app/navigation';
	import { base } from '$app/paths';
	import {
		Search,
		LayoutDashboard,
		Layers,
		ListChecks,
		Cpu,
		FlaskConical,
		TrendingUp,
		Bell,
		KeyRound,
		Server,
		Settings,
		Plus,
		RefreshCw,
		Pause,
		Play,
		Hash,
		CornerDownLeft
	} from '@lucide/svelte';
	import { queuesStore } from '$lib/stores/queues';
	import { workersStore } from '$lib/stores/workers';
	import { requestRefresh } from '$lib/stores/live';
	import { toaster } from '$lib/stores/toaster';
	import { pauseQueue, resumeQueue } from '$lib/api/client';

	type Group = 'Task' | 'Pages' | 'Queues' | 'Workers' | 'Actions';

	interface Item {
		id: string;
		group: Group;
		label: string;
		hint?: string;
		icon: Component;
		mono?: boolean;
		/** Extra words matched by the search. */
		keywords?: string;
		run: () => void | Promise<void>;
	}

	let query = $state('');
	let selected = $state(0);
	let inputEl = $state<HTMLInputElement | null>(null);
	let listEl = $state<HTMLElement | null>(null);

	const pages: { label: string; href: string; icon: Component; keys?: string }[] = [
		{ label: 'Dashboard', href: '/', icon: LayoutDashboard, keys: 'g d home overview' },
		{ label: 'Queues', href: '/queues', icon: Layers, keys: 'g q' },
		{ label: 'Tasks', href: '/tasks', icon: ListChecks, keys: 'g t jobs' },
		{ label: 'Workers', href: '/workers', icon: Cpu, keys: 'g w' },
		{ label: 'Playground', href: '/playground', icon: FlaskConical, keys: 'enqueue send' },
		{ label: 'Autoscaling', href: '/autoscaling', icon: TrendingUp, keys: 'g a instances providers' },
		{ label: 'Alerts', href: '/alerts', icon: Bell, keys: 'notifications email' },
		{ label: 'Vaults', href: '/vaults', icon: KeyRound, keys: 'secrets' },
		{ label: 'System', href: '/system', icon: Server, keys: 'redis database env' },
		{ label: 'Settings', href: '/settings', icon: Settings, keys: 'preferences poll' }
	];

	function close() {
		paletteOpen.set(false);
	}

	function go(path: string) {
		close();
		goto(`${base}${path}`);
	}

	async function toggleQueue(qname: string, paused: boolean) {
		close();
		try {
			if (paused) await resumeQueue(qname);
			else await pauseQueue(qname);
			toaster.success({ title: paused ? 'Queue Resumed' : 'Queue Paused', description: qname });
			queuesStore.fetch();
			requestRefresh();
		} catch (e) {
			toaster.error({
				title: 'Error',
				description: `Failed to ${paused ? 'resume' : 'pause'} "${qname}": ${e instanceof Error ? e.message : e}`
			});
		}
	}

	// Anything without spaces that could be an id; UUID-shaped ids go first.
	const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
	const ID_LIKE = /^[\w.:-]{6,}$/;

	let allItems = $derived.by<Item[]>(() => {
		const items: Item[] = pages.map((p) => ({
			id: `page:${p.href}`,
			group: 'Pages',
			label: p.label,
			icon: p.icon,
			keywords: p.keys,
			run: () => go(p.href)
		}));
		for (const q of $queuesStore.queues) {
			items.push({
				id: `queue:${q.queue}`,
				group: 'Queues',
				label: q.queue,
				hint: q.paused ? 'Paused' : `${q.pending} pending`,
				icon: Layers,
				run: () => go(`/queues/${encodeURIComponent(q.queue)}`)
			});
		}
		for (const w of $workersStore.workers) {
			items.push({
				id: `worker:${w.worker_id}`,
				group: 'Workers',
				label: w.worker_id,
				hint: w.status,
				icon: Cpu,
				mono: true,
				run: () => go(`/workers/${encodeURIComponent(w.worker_id)}`)
			});
		}
		items.push(
			{ id: 'action:create-queue', group: 'Actions', label: 'Create queue', icon: Plus, keywords: 'new add', run: () => go('/queues?create=1') },
			{
				id: 'action:refresh',
				group: 'Actions',
				label: 'Refresh data',
				hint: 'r',
				icon: RefreshCw,
				keywords: 'reload',
				run: () => {
					close();
					requestRefresh();
				}
			}
		);
		// Pause / resume only when asked for, so they do not double every queue result.
		for (const q of $queuesStore.queues) {
			items.push({
				id: `action:toggle:${q.queue}`,
				group: 'Actions',
				label: `${q.paused ? 'Resume' : 'Pause'} queue ${q.queue}`,
				icon: q.paused ? Play : Pause,
				keywords: 'toggle-queue',
				run: () => toggleQueue(q.queue, q.paused)
			});
		}
		return items;
	});

	function score(item: Item, q: string): number {
		const label = item.label.toLowerCase();
		if (label === q) return 100;
		if (label.startsWith(q)) return 80;
		if (label.split(/[\s.:_-]/).some((w) => w.startsWith(q))) return 60;
		if (label.includes(q)) return 40;
		if (item.keywords?.toLowerCase().includes(q)) return 20;
		return 0;
	}

	const groupOrder: Group[] = ['Task', 'Pages', 'Queues', 'Workers', 'Actions'];

	let results = $derived.by<Item[]>(() => {
		const q = query.trim().toLowerCase();
		if (!q) {
			return allItems.filter((i) => i.group === 'Pages' || (i.group === 'Actions' && !i.id.startsWith('action:toggle:')));
		}
		const wantsToggle = /^(pause|resume)\b/.test(q);
		const scored = allItems
			.filter((i) => !i.id.startsWith('action:toggle:') || wantsToggle)
			.map((i) => ({ i, s: score(i, q) }))
			.filter((x) => x.s > 0);
		const out: Item[] = [];
		for (const g of groupOrder) {
			out.push(
				...scored
					.filter((x) => x.i.group === g)
					.sort((a, b) => b.s - a.s)
					.slice(0, g === 'Pages' ? 5 : 8)
					.map((x) => x.i)
			);
		}
		const raw = query.trim();
		if (ID_LIKE.test(raw)) {
			const task: Item = {
				id: 'task:' + raw,
				group: 'Task',
				label: raw,
				hint: 'Open task',
				icon: Hash,
				mono: true,
				run: () => go(`/tasks/${encodeURIComponent(raw)}`)
			};
			// A UUID is almost surely a task id; otherwise offer it after the matches.
			if (UUID.test(raw) || out.length === 0) out.unshift(task);
			else out.push({ ...task, group: 'Actions' });
		}
		return out;
	});

	// Reset on open, load lists the current page did not load.
	$effect(() => {
		if (!$paletteOpen) return;
		query = '';
		selected = 0;
		if ($queuesStore.queues.length === 0) queuesStore.fetch();
		if ($workersStore.workers.length === 0) workersStore.fetch();
		tick().then(() => inputEl?.focus());
	});

	$effect(() => {
		void query;
		selected = 0;
	});

	$effect(() => {
		// Keep the highlighted row visible while moving with the arrows.
		listEl?.querySelector(`[data-index="${selected}"]`)?.scrollIntoView({ block: 'nearest' });
	});

	function onKeydown(e: KeyboardEvent) {
		if (e.key === 'ArrowDown') {
			e.preventDefault();
			selected = results.length ? (selected + 1) % results.length : 0;
		} else if (e.key === 'ArrowUp') {
			e.preventDefault();
			selected = results.length ? (selected - 1 + results.length) % results.length : 0;
		} else if (e.key === 'Enter') {
			e.preventDefault();
			results[selected]?.run();
		}
	}
</script>

<Dialog open={$paletteOpen} onOpenChange={(d) => paletteOpen.set(d.open)} closeOnInteractOutside>
	<Portal>
		<Dialog.Backdrop class="rq-dialog-backdrop fixed inset-0 z-50" />
		<Dialog.Positioner class="fixed inset-0 z-50 flex items-start justify-center p-3 pt-[12vh]">
			<Dialog.Content class="rq-dialog w-full max-w-xl rounded-xl overflow-hidden flex flex-col max-h-[70vh]">
				<Dialog.Title class="sr-only">Command palette</Dialog.Title>
				<div class="flex items-center gap-2.5 px-4 border-b border-line-subtle">
					<Search size={16} strokeWidth={1.75} class="text-fg-subtle shrink-0" />
					<input
						bind:this={inputEl}
						bind:value={query}
						onkeydown={onKeydown}
						type="text"
						class="rq-palette-input"
						placeholder="Search pages, queues, workers, or paste a task ID…"
						aria-label="Search commands"
						role="combobox"
						aria-expanded="true"
						aria-controls="rq-palette-list"
						aria-activedescendant={results[selected] ? `rq-palette-${selected}` : undefined}
						autocomplete="off"
						spellcheck="false"
					/>
					<kbd class="rq-kbd">Esc</kbd>
				</div>
				<div bind:this={listEl} id="rq-palette-list" role="listbox" class="flex-1 overflow-y-auto p-1.5">
					{#each results as item, i (item.id)}
						{#if i === 0 || results[i - 1].group !== item.group}
							<div class="px-2.5 pt-2.5 pb-1 text-2xs font-medium text-fg-subtle">{item.group}</div>
						{/if}
						<button
							type="button"
							id="rq-palette-{i}"
							role="option"
							aria-selected={i === selected}
							data-index={i}
							class="rq-palette-item"
							onmousemove={() => (selected = i)}
							onclick={() => item.run()}
						>
							<item.icon size={16} strokeWidth={1.75} class="shrink-0 text-fg-subtle" />
							<span class="min-w-0 flex-1 truncate {item.mono ? 'font-mono text-ui' : ''}">{item.label}</span>
							{#if item.hint}<span class="shrink-0 text-xs text-fg-subtle">{item.hint}</span>{/if}
							{#if i === selected}<CornerDownLeft size={14} strokeWidth={1.75} class="shrink-0 text-fg-faint" />{/if}
						</button>
					{:else}
						<div class="px-3 py-8 text-center text-ui text-fg-subtle">No results</div>
					{/each}
				</div>
				<div class="hidden sm:flex items-center gap-4 px-4 py-2 border-t border-line-subtle text-2xs text-fg-subtle">
					<span><kbd class="rq-kbd">↑</kbd> <kbd class="rq-kbd">↓</kbd> move</span>
					<span><kbd class="rq-kbd">↵</kbd> open</span>
					<span class="ml-auto">
						<kbd class="rq-kbd">g</kbd> then <kbd class="rq-kbd">q</kbd>/<kbd class="rq-kbd">t</kbd>/<kbd class="rq-kbd">w</kbd>
						go · <kbd class="rq-kbd">/</kbd> search · <kbd class="rq-kbd">r</kbd> refresh
					</span>
				</div>
			</Dialog.Content>
		</Dialog.Positioner>
	</Portal>
</Dialog>
