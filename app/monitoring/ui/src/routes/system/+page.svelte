<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import type { RedisInfo, DatabaseInfo, SystemEnvInfo } from '$lib/api/types';
	import { getRedisInfo, getDatabaseInfo, getSystemEnv } from '$lib/api/client';
	import { settings } from '$lib/stores/settings';
	import { ChevronDown, ChevronUp } from '@lucide/svelte';
	import { Callout, Badge, Button, KeyValue, PageHeader, Panel, Shimmer, StatTile } from '$lib/components/ui';
	import type { KV } from '$lib/components/ui/KeyValue.svelte';

	let redisInfo = $state<RedisInfo | null>(null);
	let dbInfo = $state<DatabaseInfo | null>(null);
	let envVars = $state<SystemEnvInfo | null>(null);
	let loading = $state(false);
	let error = $state<string | null>(null);
	let pollInterval: ReturnType<typeof setInterval> | null = null;
	let showRaw = $state(false);

	async function loadRedisInfo() {
		try {
			redisInfo = await getRedisInfo();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load Redis info';
		}
	}

	async function loadDatabaseInfo() {
		try {
			dbInfo = await getDatabaseInfo();
		} catch (e) {
			// Database info endpoint might not be available yet
			console.warn('Failed to load database info:', e);
		}
	}

	async function loadEnvVars() {
		try {
			envVars = await getSystemEnv();
		} catch (e) {
			// Environment endpoint might not be available yet
			console.warn('Failed to load environment variables:', e);
		}
	}

	async function loadData() {
		loading = true;
		error = null;
		await Promise.all([loadRedisInfo(), loadDatabaseInfo(), loadEnvVars()]);
		loading = false;
	}

	let envText = $derived(
		envVars ? envVars.vars.map((v) => `${v.name}=${v.value}`).join('\n') : ''
	);

	let dbItems = $derived.by((): KV[] => {
		if (!dbInfo) return [];
		const items: KV[] = [{ label: 'Type', value: dbInfo.type, mono: true }];
		if (dbInfo.host) items.push({ label: 'Host', value: dbInfo.host, mono: true });
		if (dbInfo.database) items.push({ label: 'Database', value: dbInfo.database, mono: true });
		if (dbInfo.stats) {
			items.push({
				label: 'Connections',
				value: `${dbInfo.stats.open_connections} open (${dbInfo.stats.in_use} in use, ${dbInfo.stats.idle} idle)`,
				mono: true
			});
		}
		return items;
	});

	onMount(() => {
		loadData();
		pollInterval = setInterval(loadData, $settings.pollInterval * 1000);
	});

	onDestroy(() => {
		if (pollInterval) clearInterval(pollInterval);
	});
</script>

<svelte:head>
	<title>System - runqy Monitor</title>
</svelte:head>

<div class="rq-page space-y-4 md:space-y-6">
	<PageHeader title="System" subtitle="Infrastructure and connection status" onrefresh={loadData} />

	{#if error}
		<Callout>{error}</Callout>
	{/if}

	<div class="grid grid-cols-1 md:grid-cols-2 gap-6">
		<Panel title="Redis" class="min-w-0">
			{#snippet titleExtra()}
				{#if redisInfo}
					<Badge tone="success" dot>Connected</Badge>
				{:else if loading}
					<Badge tone="neutral" dot>Checking…</Badge>
				{:else}
					<Badge tone="danger" dot>Disconnected</Badge>
				{/if}
			{/snippet}

			{#if loading && !redisInfo}
				<Shimmer lines={4} />
			{:else if redisInfo}
				<KeyValue
					inline
					items={[
						{ label: 'Address', value: redisInfo.address, mono: true },
						{ label: 'Version', value: redisInfo.info.version || 'N/A', mono: true },
						{ label: 'Uptime', value: `${redisInfo.info.uptime_in_days} days` },
						{ label: 'Connected clients', value: redisInfo.info.connected_clients, mono: true }
					]}
				/>
			{:else}
				<p class="text-ui text-fg-subtle">Unable to connect to Redis</p>
			{/if}
		</Panel>

		<Panel title="Database" class="min-w-0">
			{#snippet titleExtra()}
				{#if dbInfo?.connected}
					<Badge tone="success" dot>Connected</Badge>
				{:else if loading}
					<Badge tone="neutral" dot>Checking…</Badge>
				{:else if dbInfo}
					<Badge tone="danger" dot>Disconnected</Badge>
				{:else}
					<Badge tone="neutral" dot>Info unavailable</Badge>
				{/if}
			{/snippet}

			{#if loading && !dbInfo}
				<Shimmer lines={3} />
			{:else if dbInfo}
				<KeyValue inline items={dbItems} />
			{:else}
				<p class="text-ui text-fg-subtle">Database information is not available.</p>
			{/if}
		</Panel>
	</div>

	{#if redisInfo}
		<Panel title="Redis memory">
			<div class="grid grid-cols-2 md:grid-cols-4 gap-2 md:gap-3">
				<StatTile inset label="Used memory" value={redisInfo.info.used_memory_human} />
				<StatTile inset label="Peak memory" value={redisInfo.info.used_memory_peak_human} />
				<StatTile inset label="Clients" value={redisInfo.info.connected_clients} />
				<StatTile inset label="Uptime (days)" value={redisInfo.info.uptime_in_days} />
			</div>
		</Panel>

		{#if redisInfo.cluster}
			<Panel title="Cluster">
				<div class="grid grid-cols-3 xl:grid-cols-5 gap-2 md:gap-3">
					<StatTile
						inset
						label="State"
						value={redisInfo.cluster.cluster_state}
						tone={redisInfo.cluster.cluster_state === 'ok' ? 'success' : 'danger'}
					/>
					<StatTile inset label="Slots assigned" value={redisInfo.cluster.cluster_slots_assigned} />
					<StatTile inset label="Slots OK" value={redisInfo.cluster.cluster_slots_ok} />
					<StatTile inset label="Known nodes" value={redisInfo.cluster.cluster_known_nodes} />
					<StatTile inset label="Cluster size" value={redisInfo.cluster.cluster_size} />
				</div>
			</Panel>
		{/if}

		<Panel title="Raw Redis info">
			{#snippet actions()}
				<Button
					size="sm"
					variant="ghost"
					icon={showRaw ? ChevronUp : ChevronDown}
					aria-expanded={showRaw}
					onclick={() => (showRaw = !showRaw)}>{showRaw ? 'Hide' : 'Show'}</Button
				>
			{/snippet}
			{#if showRaw}
				<pre
					class="max-h-96 bg-canvas border border-line-subtle rounded-md p-3 text-xs font-mono text-fg-muted overflow-auto">{redisInfo.raw_info}</pre>
			{:else}
				<p class="text-ui text-fg-subtle">Output of the Redis INFO command.</p>
			{/if}
		</Panel>
	{/if}

	<Panel
		title="Environment variables"
		description="Read-only view of the server's runqy configuration (effective values, defaults included). Secrets are masked."
	>
		{#if envVars}
			<pre
				class="max-h-[32rem] bg-canvas border border-line-subtle rounded-md p-3 text-xs font-mono leading-relaxed text-fg-muted overflow-auto">{envText}</pre>
		{:else}
			<p class="text-ui text-fg-subtle">Environment information is not available.</p>
		{/if}
	</Panel>
</div>
