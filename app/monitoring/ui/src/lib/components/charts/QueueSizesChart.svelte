<script lang="ts">
	import { Chart, Bars, Axis, Svg, Tooltip } from 'layerchart';
	import { scaleBand, scaleLinear } from 'd3-scale';
	import { queuesStore } from '$lib/stores/queues';
	import { goto } from '$app/navigation';
	import { base } from '$app/paths';
	import { Panel, Shimmer } from '$lib/components/ui';

	interface Props {
		height?: number;
		maxQueues?: number;
	}

	let { height = 250, maxQueues = 8 }: Props = $props();

	// Colour per state: text-* classes, applied to bars through fill="currentColor";
	// bg-* for legend and tooltip dots.
	const stateColors = {
		// Pending is normal work: neutral grey. Retry is the anomaly.
		pending: { text: 'text-fg-muted', bg: 'bg-fg-muted' },
		active: { text: 'text-primary-500', bg: 'bg-primary-500' },
		retry: { text: 'text-warning-500', bg: 'bg-warning-500' }
	};

	const states = ['pending', 'active', 'retry'] as const;

	const tickLabelProps = { style: 'fill: var(--color-fg-subtle); stroke: none; font-size: 11px;' };

	const stateLabels = {
		pending: 'Pending',
		active: 'Active',
		retry: 'Retry'
	};

	interface ChartDataPoint {
		queue: string;
		state: string;
		value: number;
	}

	interface ChartData {
		flattened: ChartDataPoint[];
		queues: string[];
	}

	// Transform queue data into chart format
	let chartData = $derived.by((): ChartData => {
		const queues = $queuesStore.queues;
		if (queues.length === 0) return { flattened: [], queues: [] };

		// Sort by total tasks and take top N
		const sorted = [...queues]
			.map(q => ({
				queue: q.queue,
				pending: q.pending || 0,
				active: q.active || 0,
				retry: q.retry || 0,
				total: (q.pending || 0) + (q.active || 0) + (q.retry || 0)
			}))
			.sort((a, b) => b.total - a.total)
			.slice(0, maxQueues);

		// Flatten for grouped bar chart
		const flattened: ChartDataPoint[] = [];
		for (const q of sorted) {
			flattened.push({ queue: q.queue, state: 'pending', value: q.pending });
			flattened.push({ queue: q.queue, state: 'active', value: q.active });
			flattened.push({ queue: q.queue, state: 'retry', value: q.retry });
		}

		return { flattened, queues: sorted.map(q => q.queue) };
	});

	// Calculate max value for y domain
	// Every queue empty (nothing pending, active or retrying): no flat chart.
	let allEmpty = $derived(chartData.flattened.every((d) => !d.value));

	let maxValue = $derived(
		Math.max(1, ...chartData.flattened.map(d => d.value))
	);

	function formatNumber(val: number): string {
		if (val >= 1000000) return (val / 1000000).toFixed(1) + 'M';
		if (val >= 1000) return (val / 1000).toFixed(1) + 'K';
		return val.toString();
	}

	function truncateQueueName(name: string): { line1: string; line2: string } {
		const dotIndex = name.indexOf('.');
		const parent = dotIndex >= 0 ? name.slice(0, dotIndex) : name;
		const sub = dotIndex >= 0 ? name.slice(dotIndex) : '';

		const parts = parent.split('-');
		const shortParent = parts.length >= 2
			? parts.map(p => p.slice(0, 2)).join('-')
			: parent.slice(0, 4);

		const shortSub = sub ? '.' + sub.slice(1, 5) : '';

		return { line1: shortParent, line2: shortSub };
	}

	function handleQueueClick(qname: string) {
		goto(`${base}/queues/${encodeURIComponent(qname)}`);
	}
</script>

<Panel title="Live Queue Sizes" class="min-w-0" bodyClass="min-w-0">
	{#snippet actions()}
		{#if $queuesStore.queues.length > maxQueues}
			<span class="text-xs text-fg-subtle">Top {maxQueues} of {$queuesStore.queues.length} queues</span>
		{/if}
	{/snippet}

	{#if $queuesStore.loading && $queuesStore.queues.length === 0}
		<div style="height: {height}px">
			<Shimmer class="h-full w-full" />
		</div>
	{:else if $queuesStore.queues.length === 0}
		<div class="flex h-20 items-center justify-center">
			<p class="text-ui text-fg-subtle">No queues available</p>
		</div>
	{:else if allEmpty}
		<div class="flex h-20 items-center justify-center">
			<p class="text-ui text-fg-subtle">All queues are empty</p>
		</div>
	{:else if chartData.queues.length > 0}
		<div class="w-full min-w-0 overflow-hidden" style="height: {height}px">
			<Chart
				data={chartData.flattened}
				x={(d: ChartDataPoint) => d.queue}
				xScale={scaleBand().padding(0.3)}
				xDomain={chartData.queues}
				x1={(d: ChartDataPoint) => d.state}
				x1Scale={scaleBand().padding(0.12)}
				x1Domain={[...states]}
				x1Range={({ xScale }) => [0, xScale.bandwidth?.() ?? 0]}
				y={(d: ChartDataPoint) => d.value}
				yScale={scaleLinear()}
				yDomain={[0, maxValue]}
				yNice
				padding={{ top: 12, right: 12, bottom: 40, left: 44 }}
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

					<!-- One bar per queue and state, grouped within the queue band -->
					{#each chartData.queues as queue (queue)}
						{@const queueData = chartData.flattened.filter((d) => d.queue === queue)}
						{#each states as state (state)}
							{@const d = queueData.find((x) => x.state === state)}
							{#if d && d.value > 0}
								<Bars
									data={[d]}
									radius={2}
									fill="currentColor"
									onclick={() => handleQueueClick(queue)}
									class="{stateColors[state].text} cursor-pointer hover:opacity-80 transition-opacity"
								/>
							{/if}
						{/each}
					{/each}

					<Axis placement="bottom" tickLength={0}>
						<svelte:fragment slot="tickLabel" let:labelProps>
							{@const label = truncateQueueName(labelProps.value)}
							<text
								x={labelProps.x}
								y={labelProps.y}
								dy={6}
								text-anchor="middle"
								dominant-baseline="hanging"
								style="fill: var(--color-fg-subtle); font-size: 11px;"
							>
								<tspan x={labelProps.x}>{label.line1}</tspan>
								{#if label.line2}
									<tspan x={labelProps.x} dy="1.2em" style="fill: var(--color-fg-faint);">{label.line2}</tspan>
								{/if}
							</text>
						</svelte:fragment>
					</Axis>
				</Svg>

				<Tooltip.Root variant="none" let:data>
					{#if data}
						<div class="rounded-md border border-line bg-overlay px-2.5 py-2 text-xs text-fg" style="box-shadow: var(--shadow-overlay)">
							<div class="font-medium mb-1">{data.queue}</div>
							<div class="flex items-center justify-between gap-4">
								<span class="flex items-center gap-1.5 text-fg-muted">
									<span class="h-2 w-2 rounded-full {stateColors[data.state as keyof typeof stateColors]?.bg}"></span>
									{stateLabels[data.state as keyof typeof stateLabels]}
								</span>
								<span class="tabular-nums">{formatNumber(data.value)}</span>
							</div>
						</div>
					{/if}
				</Tooltip.Root>
			</Chart>
		</div>

		<div class="flex items-center justify-center gap-4 mt-3 flex-wrap text-xs text-fg-muted">
			{#each states as state (state)}
				<span class="flex items-center gap-1.5">
					<span class="h-2 w-2 rounded-full {stateColors[state].bg}"></span>
					{stateLabels[state]}
				</span>
			{/each}
		</div>
	{/if}
</Panel>
