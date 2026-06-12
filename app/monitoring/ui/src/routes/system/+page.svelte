<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import type { RedisInfo, DatabaseInfo, SystemEnvInfo } from '$lib/api/types';
	import { getRedisInfo, getDatabaseInfo, getSystemEnv } from '$lib/api/client';
	import { settings } from '$lib/stores/settings';

	let redisInfo = $state<RedisInfo | null>(null);
	let dbInfo = $state<DatabaseInfo | null>(null);
	let envVars = $state<SystemEnvInfo | null>(null);
	let loading = $state(false);
	let error = $state<string | null>(null);
	let pollInterval: ReturnType<typeof setInterval> | null = null;
	let refreshing = $state(false);

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

	async function handleRefresh() {
		refreshing = true;
		await loadData();
		setTimeout(() => { refreshing = false; }, 500);
	}

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

<div class="rq-page space-y-8">
	<!-- Header -->
	<div class="flex items-center justify-between">
		<div>
			<h1 class="rq-page-title">System</h1>
			<p class="text-surface-500">Infrastructure and connection status</p>
		</div>
		<button type="button" class="rq-btn-primary {refreshing ? 'refresh-spinning' : ''}" onclick={handleRefresh}>
			<svg class="w-4 h-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path
					stroke-linecap="round"
					stroke-linejoin="round"
					stroke-width="2"
					d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
				/>
			</svg>
			Refresh
		</button>
	</div>

	<!-- Error State -->
	{#if error}
		<div class="rq-card p-4">
			<p class="text-error-500">{error}</p>
		</div>
	{/if}

	<!-- Status Cards -->
	<div class="grid grid-cols-1 md:grid-cols-2 gap-6">
		<!-- Redis Status -->
		<div class="rq-card p-6">
			<div class="flex items-center justify-between mb-4">
				<h3 class="text-lg font-semibold flex items-center gap-2">
					<svg class="w-5 h-5 text-error-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01"
						/>
					</svg>
					Redis
				</h3>
				{#if redisInfo}
					<span class="badge preset-filled-success-500 text-xs">Connected</span>
				{:else if loading}
					<span class="badge preset-filled-surface-500 text-xs">Checking...</span>
				{:else}
					<span class="badge preset-filled-error-500 text-xs">Disconnected</span>
				{/if}
			</div>
			{#if loading && !redisInfo}
				<div class="animate-pulse space-y-3">
					{#each [1, 2, 3, 4] as i (i)}
						<div class="h-4 bg-surface-300 dark:bg-surface-600 rounded"></div>
					{/each}
				</div>
			{:else if redisInfo}
				<div class="space-y-3 text-sm">
					<div class="flex justify-between">
						<span class="text-surface-500">Address</span>
						<span class="font-mono">{redisInfo.address}</span>
					</div>
					<div class="flex justify-between">
						<span class="text-surface-500">Version</span>
						<span class="font-mono">{redisInfo.info.version || 'N/A'}</span>
					</div>
					<div class="flex justify-between">
						<span class="text-surface-500">Uptime</span>
						<span>{redisInfo.info.uptime_in_days} days</span>
					</div>
					<div class="flex justify-between">
						<span class="text-surface-500">Connected Clients</span>
						<span class="font-mono">{redisInfo.info.connected_clients}</span>
					</div>
				</div>
			{:else}
				<p class="text-surface-500 text-sm">Unable to connect to Redis</p>
			{/if}
		</div>

		<!-- Database Status -->
		<div class="rq-card p-6">
			<div class="flex items-center justify-between mb-4">
				<h3 class="text-lg font-semibold flex items-center gap-2">
					<svg class="w-5 h-5 text-primary-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M4 7v10c0 2.21 3.582 4 8 4s8-1.79 8-4V7M4 7c0 2.21 3.582 4 8 4s8-1.79 8-4M4 7c0-2.21 3.582-4 8-4s8 1.79 8 4m0 5c0 2.21-3.582 4-8 4s-8-1.79-8-4"
						/>
					</svg>
					Database
				</h3>
				{#if dbInfo?.connected}
					<span class="badge preset-filled-success-500 text-xs">Connected</span>
				{:else if loading}
					<span class="badge preset-filled-surface-500 text-xs">Checking...</span>
				{:else if dbInfo}
					<span class="badge preset-filled-error-500 text-xs">Disconnected</span>
				{:else}
					<span class="badge preset-filled-surface-500 text-xs">Info unavailable</span>
				{/if}
			</div>
			{#if loading && !dbInfo}
				<div class="animate-pulse space-y-3">
					{#each [1, 2, 3] as i (i)}
						<div class="h-4 bg-surface-300 dark:bg-surface-600 rounded"></div>
					{/each}
				</div>
			{:else if dbInfo}
				<div class="space-y-3 text-sm">
					<div class="flex justify-between">
						<span class="text-surface-500">Type</span>
						<span class="font-mono">{dbInfo.type}</span>
					</div>
					{#if dbInfo.host}
						<div class="flex justify-between">
							<span class="text-surface-500">Host</span>
							<span class="font-mono">{dbInfo.host}</span>
						</div>
					{/if}
					{#if dbInfo.database}
						<div class="flex justify-between">
							<span class="text-surface-500">Database</span>
							<span class="font-mono">{dbInfo.database}</span>
						</div>
					{/if}
					{#if dbInfo.stats}
						<div class="flex justify-between">
							<span class="text-surface-500">Connections</span>
							<span class="font-mono">{dbInfo.stats.open_connections} open ({dbInfo.stats.in_use} in use, {dbInfo.stats.idle} idle)</span>
						</div>
					{/if}
				</div>
			{:else}
				<p class="text-surface-500 text-sm">
					Database information is not available.
				</p>
			{/if}
		</div>
	</div>

	<!-- Memory Info -->
	{#if redisInfo}
		<div class="rq-card p-6">
			<h3 class="text-lg font-semibold mb-4 flex items-center gap-2">
				<svg class="w-5 h-5 text-warning-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M9 17v-2m3 2v-4m3 4v-6m2 10H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
					/>
				</svg>
				Redis Memory
			</h3>
			<div class="grid grid-cols-2 md:grid-cols-4 gap-4">
				<div class="rq-metric-box text-center">
					<div class="text-2xl font-bold text-primary-500">{redisInfo.info.used_memory_human}</div>
					<div class="text-xs text-surface-500">Used Memory</div>
				</div>
				<div class="rq-metric-box text-center">
					<div class="text-2xl font-bold text-warning-500">{redisInfo.info.used_memory_peak_human}</div>
					<div class="text-xs text-surface-500">Peak Memory</div>
				</div>
				<div class="rq-metric-box text-center">
					<div class="text-2xl font-bold text-success-500">{redisInfo.info.connected_clients}</div>
					<div class="text-xs text-surface-500">Clients</div>
				</div>
				<div class="rq-metric-box text-center">
					<div class="text-2xl font-bold">{redisInfo.info.uptime_in_days}</div>
					<div class="text-xs text-surface-500">Uptime (days)</div>
				</div>
			</div>
		</div>

		<!-- Cluster Info (if enabled) -->
		{#if redisInfo.cluster}
			<div class="rq-card p-6">
				<h3 class="text-lg font-semibold mb-4 flex items-center gap-2">
					<svg class="w-5 h-5 text-success-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10"
						/>
					</svg>
					Cluster
				</h3>
				<div class="grid grid-cols-2 md:grid-cols-5 gap-4">
					<div class="rq-metric-box text-center">
						<div class="text-lg font-bold {redisInfo.cluster.cluster_state === 'ok' ? 'text-success-500' : 'text-error-500'}">
							{redisInfo.cluster.cluster_state}
						</div>
						<div class="text-xs text-surface-500">State</div>
					</div>
					<div class="rq-metric-box text-center">
						<div class="text-lg font-bold">{redisInfo.cluster.cluster_slots_assigned}</div>
						<div class="text-xs text-surface-500">Slots Assigned</div>
					</div>
					<div class="rq-metric-box text-center">
						<div class="text-lg font-bold">{redisInfo.cluster.cluster_slots_ok}</div>
						<div class="text-xs text-surface-500">Slots OK</div>
					</div>
					<div class="rq-metric-box text-center">
						<div class="text-lg font-bold">{redisInfo.cluster.cluster_known_nodes}</div>
						<div class="text-xs text-surface-500">Known Nodes</div>
					</div>
					<div class="rq-metric-box text-center">
						<div class="text-lg font-bold">{redisInfo.cluster.cluster_size}</div>
						<div class="text-xs text-surface-500">Cluster Size</div>
					</div>
				</div>
			</div>
		{/if}

		<!-- Raw Info -->
		<div class="rq-card p-6">
			<details>
				<summary class="cursor-pointer text-lg font-semibold">Raw Redis Info</summary>
				<pre
					class="mt-4 p-4 rounded text-sm font-mono overflow-x-auto max-h-96 scrollbar-thin bg-surface-900 text-surface-100">{redisInfo.raw_info}</pre>
			</details>
		</div>
	{/if}

	<!-- Environment Variables (read-only, secrets masked) -->
	<div class="rq-card p-6">
		<h3 class="text-lg font-semibold mb-2 flex items-center gap-2">
			<svg class="w-5 h-5 text-tertiary-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path
					stroke-linecap="round"
					stroke-linejoin="round"
					stroke-width="2"
					d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4"
				/>
			</svg>
			Environment Variables
		</h3>
		<p class="text-surface-500 text-sm mb-4">
			Read-only view of the server's runqy configuration (effective values, defaults included).
			Secrets are masked.
		</p>
		{#if envVars}
			<textarea
				readonly
				rows="24"
				class="textarea w-full p-4 rounded text-sm font-mono leading-relaxed scrollbar-thin bg-surface-900 text-surface-100"
				value={envText}
			></textarea>
		{:else}
			<p class="text-surface-500 text-sm">Environment information is not available.</p>
		{/if}
	</div>
</div>
