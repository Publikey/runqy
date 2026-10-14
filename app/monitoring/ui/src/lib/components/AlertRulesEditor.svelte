<script lang="ts">
	import type { AlertRule, AlertRuleType } from '$lib/api/types';

	let {
		rules = $bindable([]),
		override = false,
		disabled = false
	}: {
		rules: AlertRule[];
		// Queue override mode: rules replace the global rule of the same type, and can turn it off.
		override?: boolean;
		disabled?: boolean;
	} = $props();

	const ruleTypes: { value: AlertRuleType; label: string; hint: string }[] = [
		{ value: 'no_workers', label: 'No workers', hint: 'No live worker serves the queue' },
		{ value: 'latency', label: 'Latency', hint: 'Oldest pending job waited longer than the threshold' },
		{ value: 'pending', label: 'Backlog', hint: 'More jobs pending than the threshold' },
		{ value: 'worker_unhealthy', label: 'Unhealthy worker', hint: 'A worker reports a degraded state' },
		{ value: 'autoscale_failure', label: 'Autoscaling failure', hint: 'Provisioning failed or an instance was destroyed for failure' }
	];

	let usedTypes = $derived(new Set(rules.map((r) => r.type)));
	let nextType = $derived(ruleTypes.find((t) => !usedTypes.has(t.value))?.value);

	function addRule() {
		if (!nextType) return;
		const rule: AlertRule = { type: nextType };
		if (nextType === 'latency') rule.max_latency = '10m';
		if (nextType === 'pending') rule.threshold = 100;
		if (nextType === 'no_workers') rule.for = '2m';
		rules = [...rules, rule];
	}

	function removeRule(index: number) {
		rules = rules.filter((_, i) => i !== index);
	}

	function hintFor(type: AlertRuleType): string {
		return ruleTypes.find((t) => t.value === type)?.hint ?? '';
	}
</script>

<div class="space-y-3">
	{#if rules.length === 0}
		<p class="text-sm text-surface-500">
			{override ? 'No overrides: this queue uses the global rules.' : 'No rules defined.'}
		</p>
	{:else}
		{#each rules as rule, index (index)}
			<div class="rq-metric-box space-y-2">
				<div class="flex flex-wrap items-end gap-2">
					<label class="label flex-1 min-w-[160px]">
						<span class="label-text text-xs">Rule</span>
						<select bind:value={rule.type} class="select" {disabled}>
							{#each ruleTypes as t (t.value)}
								<option value={t.value} disabled={t.value !== rule.type && usedTypes.has(t.value)}>{t.label}</option>
							{/each}
						</select>
					</label>
					{#if !rule.disabled}
						{#if rule.type === 'latency'}
							<label class="label w-28">
								<span class="label-text text-xs">Max latency</span>
								<input type="text" bind:value={rule.max_latency} placeholder="10m" class="input" {disabled} />
							</label>
						{:else if rule.type === 'pending'}
							<label class="label w-28">
								<span class="label-text text-xs">Threshold</span>
								<input type="number" bind:value={rule.threshold} min="1" class="input" {disabled} />
							</label>
						{/if}
						{#if rule.type !== 'autoscale_failure'}
							<label class="label w-24">
								<span class="label-text text-xs">For</span>
								<input type="text" bind:value={rule.for} placeholder="0s" class="input" {disabled} />
							</label>
						{/if}
					{/if}
					<button
						type="button"
						class="btn-icon preset-tonal"
						onclick={() => removeRule(index)}
						{disabled}
						title="Remove rule"
						aria-label="Remove rule"
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
						</svg>
					</button>
				</div>
				<div class="flex flex-wrap items-center gap-x-4 gap-y-1">
					<span class="text-xs text-surface-500">{hintFor(rule.type)}</span>
					{#if rule.type === 'no_workers' && !rule.disabled}
						<label class="flex items-center gap-2 text-xs text-surface-400 cursor-pointer">
							<input type="checkbox" bind:checked={rule.when_idle} class="checkbox" {disabled} />
							Also when nothing is pending
						</label>
					{/if}
					{#if override}
						<label class="flex items-center gap-2 text-xs text-surface-400 cursor-pointer">
							<input type="checkbox" bind:checked={rule.disabled} class="checkbox" {disabled} />
							Turn this rule off for this queue
						</label>
					{/if}
				</div>
			</div>
		{/each}
	{/if}
	{#if nextType}
		<button type="button" class="rq-btn-ghost" onclick={addRule} {disabled}>
			<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
			</svg>
			Add rule
		</button>
	{/if}
</div>
