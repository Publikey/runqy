<script lang="ts">
	import { Chart, Area, Axis, Svg, Tooltip, Highlight } from 'layerchart';
	import { scaleTime, scaleLinear } from 'd3-scale';
	import { stack, stackOrderNone, stackOffsetNone, curveMonotoneX } from 'd3-shape';
	import {
		queueStatsStore,
		dailyThroughputData,
		todayStats,
		availableQueues,
		type TimeRangeKey
	} from '$lib/stores/metrics';
	import { queuesStore } from '$lib/stores/queues';
	import { onMount, onDestroy } from 'svelte';
	import { settings } from '$lib/stores/settings';
	import { Panel, Segmented, Shimmer } from '$lib/components/ui';

	interface Props {
		height?: number;
	}

	let { height = 300 }: Props = $props();

	let pollInterval: ReturnType<typeof setInterval> | null = null;

	// Time range switcher
	const timeRanges: { value: TimeRangeKey; label: string }[] = [
		{ value: 'today', label: 'Today' },
		{ value: '7d', label: '7D' },
		{ value: '30d', label: '30D' }
	];

	// Series colours (classes, applied through fill="currentColor").
	const seriesClass = ['text-success-500', 'text-error-500'];

	const tickLabelProps = { style: 'fill: var(--color-fg-subtle); stroke: none; font-size: 11px;' };

	function selectTimeRange(range: TimeRangeKey) {
		queueStatsStore.setTimeRange(range);
	}

	function selectQueue(queue: string | null) {
		queueStatsStore.setQueue(queue);
	}

	// Prepare data for the line/area chart (7D/30D views)
	let areaChartData = $derived.by(() => {
		const data = $dailyThroughputData;
		if (data.length === 0)
			return { series: [], xDomain: [new Date(), new Date()], yDomain: [0, 1], data: [] };

		// Convert date strings to Date objects
		const withDates = data.map((d) => ({
			...d,
			timestamp: new Date(d.date + 'T12:00:00') // Noon to avoid timezone issues
		}));

		// Stack generator
		const stackGen = stack<(typeof withDates)[0]>()
			.keys(['processed', 'failed'])
			.order(stackOrderNone)
			.offset(stackOffsetNone);

		const series = stackGen(withDates);

		// Calculate domains
		const xDomain = [withDates[0].timestamp, withDates[withDates.length - 1].timestamp];
		const maxY = Math.max(...series.flatMap((s) => s.flatMap((d) => d[1])));
		const yDomain = [0, Math.max(maxY, 10)]; // Ensure at least some range

		return { series, xDomain, yDomain, data: withDates };
	});

	// Prepare data for the bar chart (Today view)
	let barChartData = $derived.by(() => {
		const stats = $todayStats;
		return [
			{ label: 'Processed', value: stats.processed, color: 'bg-success-500' },
			{ label: 'Failed', value: stats.failed, color: 'bg-error-500' }
		];
	});

	let barMaxValue = $derived(Math.max(1, ...barChartData.map((d) => d.value)));

	// Format functions
	function formatDate(date: Date): string {
		return date.toLocaleDateString([], { month: 'short', day: 'numeric' });
	}

	function formatNumber(val: number): string {
		if (val >= 1000000) return (val / 1000000).toFixed(1) + 'M';
		if (val >= 1000) return (val / 1000).toFixed(1) + 'K';
		return val.toFixed(0);
	}

	function formatLargeNumber(val: number): string {
		return val.toLocaleString();
	}

	onMount(() => {
		queueStatsStore.fetch();
		queuesStore.fetch();
		pollInterval = setInterval(() => {
			queueStatsStore.fetch();
			queuesStore.fetch();
		}, $settings.pollInterval * 1000);
	});

	onDestroy(() => {
		if (pollInterval) clearInterval(pollInterval);
	});
</script>

<Panel title="Task History" class="min-w-0" bodyClass="min-w-0">
	{#snippet actions()}
		{#if $availableQueues.length > 1}
			<select
				class="select h-8 w-36 text-ui"
				aria-label="Queue"
				value={$queueStatsStore.selectedQueue || ''}
				onchange={(e) => selectQueue(e.currentTarget.value || null)}
			>
				<option value="">All queues</option>
				{#each $availableQueues as queue (queue)}
					<option value={queue}>{queue}</option>
				{/each}
			</select>
		{/if}
		<Segmented
			items={timeRanges}
			label="Time range"
			value={$queueStatsStore.selectedTimeRange}
			onchange={selectTimeRange}
		/>
	{/snippet}

	{#if $queueStatsStore.loading && Object.keys($queueStatsStore.rawStats).length === 0}
		<div class="flex flex-col justify-end gap-3" style="height: {height}px">
			<Shimmer class="h-full w-full" />
		</div>
	{:else if $queueStatsStore.error}
		<div class="flex items-center justify-center" style="height: {height}px">
			<p class="text-ui text-error-300">{$queueStatsStore.error}</p>
		</div>
	{:else if $queueStatsStore.selectedTimeRange === 'today'}
		<!-- Today: two bars -->
		<div style="height: {height}px" class="flex items-end justify-center pb-2">
			<div class="flex gap-12 sm:gap-16 items-end">
				{#each barChartData as item (item.label)}
					<div class="flex flex-col items-center">
						<div
							class="w-16 sm:w-20 rounded-t-sm transition-all duration-300 {item.color}"
							style="height: {Math.max(20, (item.value / Math.max(barMaxValue, 1)) * (height - 100))}px;"
						></div>
						<div class="mt-3 text-center">
							<div class="text-xl font-semibold tracking-tight tabular-nums text-fg">{formatLargeNumber(item.value)}</div>
							<div class="text-xs text-fg-subtle">{item.label}</div>
						</div>
					</div>
				{/each}
			</div>
		</div>
	{:else if $dailyThroughputData.length === 0}
		<div class="flex items-center justify-center" style="height: {height}px">
			<p class="text-ui text-fg-subtle">No historical data available</p>
		</div>
	{:else}
		<!-- 7D/30D: stacked areas -->
		<div class="w-full min-w-0 overflow-hidden" style="height: {height}px">
			<Chart
				data={areaChartData.data}
				x={(d: { timestamp: Date }) => d.timestamp}
				xScale={scaleTime()}
				xDomain={areaChartData.xDomain}
				yScale={scaleLinear()}
				yDomain={areaChartData.yDomain}
				yNice
				padding={{ top: 12, right: 12, bottom: 28, left: 44 }}
				tooltip={{ mode: 'bisect-x' }}
			>
				<Svg>
					<Axis
						placement="left"
						format={formatNumber}
						ticks={5}
						tickLength={0}
						grid={{ class: 'stroke-line-subtle' }}
						{tickLabelProps}
					/>
					<Axis placement="bottom" format={formatDate} ticks={6} tickLength={0} {tickLabelProps} />

					{#each areaChartData.series as s, i (i)}
						<Area
							data={s}
							x={(d) => d.data.timestamp}
							y0={(d) => d[0]}
							y1={(d) => d[1]}
							fill="currentColor"
							fillOpacity={0.35}
							line={{ stroke: 'currentColor', strokeWidth: 1.5, class: seriesClass[i] }}
							class={seriesClass[i]}
							curve={curveMonotoneX}
							tweened
						/>
					{/each}

					<Highlight lines={{ class: 'stroke-line-strong' }} />
				</Svg>

				<Tooltip.Root variant="none" let:data>
					{#if data}
						<div class="rounded-md border border-line bg-overlay px-2.5 py-2 text-xs text-fg" style="box-shadow: var(--shadow-overlay)">
							<div class="font-medium mb-1">{data.date}</div>
							<div class="flex items-center justify-between gap-4">
								<span class="flex items-center gap-1.5 text-fg-muted"
									><span class="h-2 w-2 rounded-full bg-success-500"></span>Processed</span
								>
								<span class="tabular-nums">{formatLargeNumber(data.processed)}</span>
							</div>
							<div class="flex items-center justify-between gap-4">
								<span class="flex items-center gap-1.5 text-fg-muted"
									><span class="h-2 w-2 rounded-full bg-error-500"></span>Failed</span
								>
								<span class="tabular-nums">{formatLargeNumber(data.failed)}</span>
							</div>
						</div>
					{/if}
				</Tooltip.Root>
			</Chart>
		</div>
	{/if}

	<div class="flex items-center justify-center gap-4 mt-3 text-xs text-fg-muted">
		<span class="flex items-center gap-1.5"><span class="h-2 w-2 rounded-full bg-success-500"></span>Processed</span>
		<span class="flex items-center gap-1.5"><span class="h-2 w-2 rounded-full bg-error-500"></span>Failed</span>
	</div>
</Panel>
