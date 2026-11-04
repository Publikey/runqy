<script lang="ts">
	import { settings } from '$lib/stores/settings';
	import { RotateCcw } from '@lucide/svelte';
	import { Button, Field, PageHeader, Panel, Toggle } from '$lib/components/ui';

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

<div class="rq-page space-y-6 max-w-2xl">
	<PageHeader title="Settings" subtitle="Preferences for this browser" />

	<Panel title="Data refresh">
		<Field label="Auto-refresh interval" for="poll-interval" hint="How often to refresh data from the server">
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
		</Field>
	</Panel>

	<Panel title="Navigation">
		<Toggle
			label="Collapsed sidebar"
			description="Show only icons in the sidebar"
			checked={$settings.sidebarCollapsed}
			onchange={() => settings.toggleSidebar()}
		/>
	</Panel>

	<Panel title="Reset">
		<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
			<p class="text-ui text-fg-muted">Reset all settings to their default values</p>
			<Button variant="secondary" icon={RotateCcw} onclick={() => settings.reset()}>Reset to defaults</Button>
		</div>
	</Panel>
</div>
