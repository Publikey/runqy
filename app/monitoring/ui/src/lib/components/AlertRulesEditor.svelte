<script lang="ts">
	import type { AlertRule, AlertRuleType } from '$lib/api/types';
	import { Plus, X } from '@lucide/svelte';
	import { Button, Field, IconButton } from '$lib/components/ui';

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

	// Unique per instance: the editor can be on the page and in a modal at once.
	const uid = $props.id();

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
		<p class="text-ui text-fg-subtle">
			{override ? 'No overrides: this queue uses the global rules.' : 'No rules defined.'}
		</p>
	{:else}
		{#each rules as rule, index (index)}
			{@const id = `${uid}-rule-${index}`}
			<div class="rounded-md border border-line-subtle bg-canvas p-3 space-y-2">
				<div class="flex flex-wrap items-end gap-2">
					<Field label="Rule" for="{id}-type" class="flex-1 min-w-40">
						<select id="{id}-type" bind:value={rule.type} class="select" {disabled}>
							{#each ruleTypes as t (t.value)}
								<option value={t.value} disabled={t.value !== rule.type && usedTypes.has(t.value)}>{t.label}</option>
							{/each}
						</select>
					</Field>
					{#if !rule.disabled}
						{#if rule.type === 'latency'}
							<Field label="Max latency" for="{id}-latency" class="w-28">
								<input id="{id}-latency" type="text" bind:value={rule.max_latency} placeholder="10m" class="input" {disabled} />
							</Field>
						{:else if rule.type === 'pending'}
							<Field label="Threshold" for="{id}-threshold" class="w-28">
								<input id="{id}-threshold" type="number" bind:value={rule.threshold} min="1" class="input" {disabled} />
							</Field>
						{/if}
						{#if rule.type !== 'autoscale_failure'}
							<Field label="For" for="{id}-for" class="w-24">
								<input
									id="{id}-for"
									type="text"
									bind:value={rule.for}
									placeholder={rule.type === 'worker_unhealthy' ? '1m' : '0s'}
									title="How long the condition must hold before the alert fires"
									class="input"
									{disabled}
								/>
							</Field>
							<Field label="Resolve after" for="{id}-resolve" class="w-28">
								<input
									id="{id}-resolve"
									type="text"
									bind:value={rule.resolve_after}
									placeholder="5m"
									title="How long the condition must stay clear before the alert is resolved (0s = at once)"
									class="input"
									{disabled}
								/>
							</Field>
						{/if}
					{/if}
					<IconButton
						icon={X}
						label="Remove rule"
						size="md"
						danger
						{disabled}
						onclick={() => removeRule(index)}
					/>
				</div>
				<div class="flex flex-wrap items-center gap-x-4 gap-y-1">
					<span class="text-xs text-fg-subtle">{hintFor(rule.type)}</span>
					{#if rule.type === 'no_workers' && !rule.disabled}
						<label class="flex items-center gap-2 text-xs text-fg-muted cursor-pointer">
							<input type="checkbox" bind:checked={rule.when_idle} class="checkbox" {disabled} />
							Also when nothing is pending
						</label>
					{/if}
					{#if override}
						<label class="flex items-center gap-2 text-xs text-fg-muted cursor-pointer">
							<input type="checkbox" bind:checked={rule.disabled} class="checkbox" {disabled} />
							Turn this rule off for this queue
						</label>
					{/if}
				</div>
			</div>
		{/each}
	{/if}
	{#if nextType}
		<Button variant="secondary" size="sm" icon={Plus} onclick={addRule} {disabled}>Add rule</Button>
	{/if}
</div>
