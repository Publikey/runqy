<script lang="ts">
	import { settings } from '$lib/stores/settings';

	const pollIntervals = [
		{ value: 2, label: '2 seconds' },
		{ value: 5, label: '5 seconds' },
		{ value: 10, label: '10 seconds' },
		{ value: 30, label: '30 seconds' },
		{ value: 60, label: '1 minute' }
	];

	function handlePollIntervalChange(e: Event) {
		settings.setPollInterval(parseInt((e.target as HTMLSelectElement).value));
	}
</script>

<svelte:head>
	<title>Settings - runqy Monitor</title>
</svelte:head>

<div class="rq-page max-w-2xl">
	<h1 class="rq-page-title mb-6">Settings</h1>

	<div class="space-y-8">
		<!-- Polling -->
		<div class="rq-card p-6">
			<h2 class="text-lg font-semibold mb-4">Data Refresh</h2>

			<div>
				<label class="text-sm font-medium text-surface-600 dark:text-surface-400" for="poll-interval">
					Auto-refresh interval
				</label>
				<p class="text-xs text-surface-500 mb-2">How often to refresh data from the server</p>
				<select
					id="poll-interval"
					class="select max-w-xs"
					value={$settings.pollInterval}
					onchange={handlePollIntervalChange}
				>
					{#each pollIntervals as interval (interval.value)}
						<option value={interval.value}>{interval.label}</option>
					{/each}
				</select>
			</div>
		</div>

		<!-- Sidebar -->
		<div class="rq-card p-6">
			<h2 class="text-lg font-semibold mb-4">Navigation</h2>

			<div class="flex items-center justify-between">
				<div>
					<div class="font-medium">Collapsed Sidebar</div>
					<div class="text-xs text-surface-500">Show only icons in the sidebar</div>
				</div>
				<label class="relative inline-flex items-center cursor-pointer">
					<input
						type="checkbox"
						class="sr-only peer"
						checked={$settings.sidebarCollapsed}
						onchange={() => settings.toggleSidebar()}
					/>
					<div
						class="w-11 h-6 bg-surface-300 peer-focus:outline-none peer-focus:ring-4 peer-focus:ring-primary-300 dark:peer-focus:ring-primary-800 rounded-full peer dark:bg-surface-700 peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-surface-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all dark:border-surface-600 peer-checked:bg-primary-500"
					></div>
				</label>
			</div>
		</div>

		<!-- Reset -->
		<div class="rq-card p-6">
			<h2 class="text-lg font-semibold mb-4">Reset</h2>
			<p class="text-sm text-surface-500 mb-4">Reset all settings to their default values</p>
			<button type="button" class="btn preset-outlined-error-500" onclick={() => settings.reset()}>
				Reset to Defaults
			</button>
		</div>
	</div>
</div>
