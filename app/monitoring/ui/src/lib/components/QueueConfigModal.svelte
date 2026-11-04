<script lang="ts">
	import { Plus, X } from '@lucide/svelte';
	import { Callout, Modal, Segmented, Field, Button, IconButton, Toggle } from '$lib/components/ui';
	import type {
		QueueConfigDetail,
		DeploymentConfig,
		VaultSummary,
		AutoscaleConfig,
		AutoscaleProvider,
		ScaleTrigger,
		ScaleTriggerType,
		AlertRule,
		QueueLimitsConfig
	} from '$lib/api/types';
	import { getVaults, getVault, getAutoscaleProviders } from '$lib/api/client';
	import AlertRulesEditor from './AlertRulesEditor.svelte';

	interface SubQueue {
		name: string;
		priority: number;
	}

	interface QueueToCreate {
		name: string;
		priority: number;
		deployment: DeploymentConfig | null;
	}

	interface Props {
		open?: boolean;
		loading?: boolean;
		mode?: 'create' | 'edit';
		config?: QueueConfigDetail | null;
		existingQueues?: string[];
		onsave?: (queues: QueueToCreate[]) => void;
		onsavesubqueue?: (parentQueue: string, subqueueName: string, priority: number) => void;
		oncancel?: () => void;
	}

	let {
		open = $bindable(false),
		loading = false,
		mode = 'create',
		config = null,
		existingQueues = [],
		onsave,
		onsavesubqueue,
		oncancel
	}: Props = $props();

	// Unique per instance: ids for label/control pairs.
	const uid = $props.id();
	const formId = `${uid}-form`;

	let createType = $state<'queue' | 'subqueue'>('queue');
	let selectedParentQueue = $state('');
	let subqueueName = $state('');
	let subqueuePriority = $state(1);
	let queueName = $state('');
	let subQueues = $state<SubQueue[]>([]);
	let gitUrl = $state('');
	let branch = $state('main');
	let codePath = $state('');
	let startupCmd = $state('');
	let deploymentMode = $state('long_running');
	let startupTimeout = $state(60);
	let redisStorage = $state(false);
	let selectedVaults = $state<Set<string>>(new Set());
	let gitTokenVault = $state('');
	let gitTokenKey = $state('');
	let availableVaults = $state<VaultSummary[]>([]);
	let selectedVaultEntries = $state<string[]>([]);
	let loadingVaults = $state(false);
	let loadingEntries = $state(false);
	let error = $state('');

	// Autoscaling state
	let autoscaleEnabled = $state(false);
	let autoscaleProvider = $state('');
	let autoscaleMinWorkers = $state(0);
	let autoscaleMaxWorkers = $state(1);
	let autoscalePollInterval = $state('30s');
	let autoscaleBootTimeout = $state('');
	let scaleUp = $state<ScaleTrigger[]>([]);
	let scaleDown = $state<ScaleTrigger[]>([]);
	let instanceGpu = $state('');
	let instanceImage = $state('');
	let instanceDiskGb = $state<number | undefined>(undefined);
	let instanceMaxPrice = $state<number | undefined>(undefined);
	let instanceMinRamGb = $state<number | undefined>(undefined);
	let instanceMinVcpu = $state<number | undefined>(undefined);
	let availableProviders = $state<AutoscaleProvider[]>([]);

	// Alert overrides for this queue
	let alertsMuted = $state(false);
	let alertRules = $state<AlertRule[]>([]);

	// Fields this form does not edit but must keep when saving an existing queue.
	let preservedLimits = $state<QueueLimitsConfig | undefined>(undefined);
	let loadingProviders = $state(false);

	const scaleUpTypes: ScaleTriggerType[] = ['no_workers', 'queue_depth', 'latency', 'schedule'];
	const scaleDownTypes: ScaleTriggerType[] = ['idle', 'schedule'];

	// Triggers that can watch only some sub-queues (empty list = every sub-queue).
	const watchesSubQueues = (t: ScaleTriggerType) => t === 'no_workers' || t === 'queue_depth' || t === 'latency';

	// "premium, vip" -> ["premium", "vip"]; tolerates full names ("inference.premium").
	function parseSubQueueList(v: string): string[] {
		const names = v
			.split(',')
			.map((s) => s.trim())
			.filter(Boolean)
			.map((s) => s.substring(s.lastIndexOf('.') + 1));
		return [...new Set(names)];
	}

	async function loadProviders() {
		loadingProviders = true;
		try {
			const response = await getAutoscaleProviders();
			availableProviders = response.providers || [];
		} catch (e) {
			console.error('Failed to load providers:', e);
			availableProviders = [];
		} finally {
			loadingProviders = false;
		}
	}

	function addScaleUp() {
		scaleUp = [...scaleUp, { trigger: 'no_workers' }];
	}

	function removeScaleUp(index: number) {
		scaleUp = scaleUp.filter((_, i) => i !== index);
	}

	function addScaleDown() {
		scaleDown = [...scaleDown, { trigger: 'idle' }];
	}

	function removeScaleDown(index: number) {
		scaleDown = scaleDown.filter((_, i) => i !== index);
	}

	let parentQueues = $derived(() => {
		const parents = new Set<string>();
		for (const q of existingQueues) {
			const dotIndex = q.lastIndexOf('.');
			if (dotIndex > 0) {
				parents.add(q.substring(0, dotIndex));
			} else {
				parents.add(q);
			}
		}
		return Array.from(parents).sort();
	});

	async function loadVaults() {
		loadingVaults = true;
		try {
			const response = await getVaults();
			availableVaults = response.vaults || [];
		} catch (e) {
			console.error('Failed to load vaults:', e);
			availableVaults = [];
		} finally {
			loadingVaults = false;
		}
	}

	async function loadVaultEntries(vaultName: string) {
		if (!vaultName) {
			selectedVaultEntries = [];
			return;
		}
		loadingEntries = true;
		try {
			const vault = await getVault(vaultName);
			selectedVaultEntries = vault.entries?.map(e => e.key) || [];
		} catch (e) {
			console.error('Failed to load vault entries:', e);
			selectedVaultEntries = [];
		} finally {
			loadingEntries = false;
		}
	}

	function toggleVault(vaultName: string) {
		const newSet = new Set(selectedVaults);
		if (newSet.has(vaultName)) {
			newSet.delete(vaultName);
		} else {
			newSet.add(vaultName);
		}
		selectedVaults = newSet;
	}

	function addSubQueue() {
		subQueues = [...subQueues, { name: '', priority: 1 }];
	}

	function removeSubQueue(index: number) {
		subQueues = subQueues.filter((_, i) => i !== index);
	}

	function handleSubmit() {
		error = '';

		if (mode === 'create' && createType === 'subqueue') {
			if (!selectedParentQueue) {
				error = 'Please select a parent queue';
				return;
			}
			if (!subqueueName.trim()) {
				error = 'Subqueue name is required';
				return;
			}
			if (!/^[a-zA-Z0-9_-]+$/.test(subqueueName.trim())) {
				error = 'Subqueue name can only contain letters, numbers, hyphens, and underscores';
				return;
			}
			if (subqueuePriority < 1) {
				error = 'Priority must be at least 1';
				return;
			}
			onsavesubqueue?.(selectedParentQueue, subqueueName.trim(), subqueuePriority);
			return;
		}

		if (!queueName.trim()) {
			error = 'Queue name is required';
			return;
		}
		if (!/^[a-zA-Z0-9_-]+$/.test(queueName.trim())) {
			error = 'Queue name can only contain letters, numbers, hyphens, and underscores';
			return;
		}

		for (const sq of subQueues) {
			if (!sq.name.trim()) {
				error = 'All subqueue names are required';
				return;
			}
			if (!/^[a-zA-Z0-9_-]+$/.test(sq.name.trim())) {
				error = 'Subqueue names can only contain letters, numbers, hyphens, and underscores';
				return;
			}
			if (sq.priority < 1) {
				error = 'All subqueue priorities must be at least 1';
				return;
			}
		}

		let deployment: DeploymentConfig | null = null;
		const hasDeploymentConfig = gitUrl.trim() || startupCmd.trim();

		if (mode === 'create' || hasDeploymentConfig) {
			if (mode === 'create' && !gitUrl.trim()) {
				error = 'Git URL is required';
				return;
			}
			if (mode === 'create' && !startupCmd.trim()) {
				error = 'Startup command is required';
				return;
			}

			if (gitUrl.trim() && startupCmd.trim()) {
				const vaultsArray = Array.from(selectedVaults);
				let gitTokenRef = '';
				if (gitTokenVault && gitTokenKey) {
					gitTokenRef = `vault://${gitTokenVault}/${gitTokenKey}`;
				}

				deployment = {
					git_url: gitUrl.trim(),
					branch: branch.trim() || 'main',
					code_path: codePath.trim() || undefined,
					startup_cmd: startupCmd.trim(),
					mode: deploymentMode,
					startup_timeout_secs: startupTimeout,
					redis_storage: redisStorage,
					vaults: vaultsArray.length > 0 ? vaultsArray : undefined,
					git_token: gitTokenRef || undefined,
					limits: preservedLimits
				};
			}
		}

		// Autoscaling: only include when enabled. Validate trigger rows.
		if (autoscaleEnabled) {
			if (!deployment) {
				error = 'Autoscaling requires a deployment configuration (Git URL and startup command)';
				return;
			}
			if (!autoscaleProvider) {
				error = 'Autoscaling requires a provider';
				return;
			}
			if (autoscaleMaxWorkers < 1) {
				error = 'Autoscaling max workers must be at least 1';
				return;
			}
			if (autoscaleMinWorkers < 0 || autoscaleMinWorkers > autoscaleMaxWorkers) {
				error = 'Autoscaling min workers must be between 0 and max workers';
				return;
			}

			for (const t of scaleUp) {
				if (t.trigger === 'queue_depth' && (t.threshold === undefined || t.threshold === null)) {
					error = 'queue_depth scale-up triggers require a threshold';
					return;
				}
				if (t.trigger === 'latency' && !t.max_latency?.trim()) {
					error = 'latency scale-up triggers require a max latency (e.g. 5m)';
					return;
				}
				if (t.trigger === 'schedule') {
					if (!t.cron?.trim()) {
						error = 'schedule scale-up triggers require a cron expression';
						return;
					}
					if (t.workers === undefined || t.workers === null) {
						error = 'schedule scale-up triggers require workers';
						return;
					}
				}
			}
			for (const t of scaleDown) {
				if (t.trigger === 'schedule' && !t.cron?.trim()) {
					error = 'schedule scale-down triggers require a cron expression';
					return;
				}
				if (t.trigger === 'idle' && !t.timeout?.trim()) {
					error = 'idle scale-down triggers require a timeout';
					return;
				}
			}

			const cleanTrigger = (t: ScaleTrigger): ScaleTrigger => {
				const out: ScaleTrigger = { trigger: t.trigger };
				if (t.trigger === 'queue_depth') out.threshold = t.threshold;
				if (t.trigger === 'latency') out.max_latency = t.max_latency?.trim();
				if (t.trigger === 'schedule') {
					out.cron = t.cron?.trim();
					if (t.workers !== undefined && t.workers !== null) out.workers = t.workers;
					if (t.duration?.trim()) out.duration = t.duration.trim();
				}
				if (t.trigger === 'idle') out.timeout = t.timeout?.trim();
				if (watchesSubQueues(t.trigger) && t.sub_queues?.length) out.sub_queues = t.sub_queues;
				return out;
			};

			const autoscale: AutoscaleConfig = {
				enabled: true,
				provider: autoscaleProvider,
				min_workers: autoscaleMinWorkers,
				max_workers: autoscaleMaxWorkers,
				poll_interval: autoscalePollInterval.trim() || '30s',
				boot_timeout: autoscaleBootTimeout.trim() || undefined,
				scale_up: scaleUp.map(cleanTrigger),
				scale_down: scaleDown.map(cleanTrigger),
				instance: {
					gpu: instanceGpu.trim() || undefined,
					image: instanceImage.trim() || undefined,
					disk_gb: instanceDiskGb,
					max_price_per_hour: instanceMaxPrice,
					min_ram_gb: instanceMinRamGb || undefined,
					min_vcpu: instanceMinVcpu || undefined
				}
			};
			deployment.autoscale = autoscale;
		}

		// Alert overrides: only stored when the queue is muted or has its own rules.
		if (alertsMuted || alertRules.length > 0) {
			if (!deployment) {
				error = 'Alert overrides require a deployment configuration (Git URL and startup command)';
				return;
			}
			for (const r of alertRules) {
				if (r.disabled) continue;
				if (r.type === 'latency' && !r.max_latency?.trim()) {
					error = 'latency alert rules require a max latency (e.g. 10m)';
					return;
				}
				if (r.type === 'pending' && !(Number(r.threshold) > 0)) {
					error = 'backlog alert rules require a positive threshold';
					return;
				}
			}
			deployment.alerts = {
				disabled: alertsMuted || undefined,
				rules: alertRules.map((r) => {
					const rule: AlertRule = { type: r.type };
					if (r.disabled) {
						rule.disabled = true;
						return rule;
					}
					if (r.for?.trim()) rule.for = r.for.trim();
					if (r.type === 'latency') rule.max_latency = r.max_latency?.trim();
					if (r.type === 'pending') rule.threshold = Number(r.threshold);
					if (r.type === 'no_workers' && r.when_idle) rule.when_idle = true;
					return rule;
				})
			};
		}

		const queuesToCreate: QueueToCreate[] = [];

		if (subQueues.length === 0) {
			queuesToCreate.push({
				name: `${queueName.trim()}.default`,
				priority: 1,
				deployment
			});
		} else {
			for (const sq of subQueues) {
				queuesToCreate.push({
					name: `${queueName.trim()}.${sq.name.trim()}`,
					priority: sq.priority,
					deployment
				});
			}
		}

		onsave?.(queuesToCreate);
	}

	function handleCancel() {
		resetForm();
		oncancel?.();
		open = false;
	}

	function resetForm() {
		createType = 'queue';
		selectedParentQueue = '';
		subqueueName = '';
		subqueuePriority = 1;
		queueName = '';
		subQueues = [];
		gitUrl = '';
		branch = 'main';
		codePath = '';
		startupCmd = '';
		deploymentMode = 'long_running';
		startupTimeout = 60;
		redisStorage = false;
		selectedVaults = new Set();
		gitTokenVault = '';
		gitTokenKey = '';
		autoscaleEnabled = false;
		autoscaleProvider = '';
		autoscaleMinWorkers = 0;
		autoscaleMaxWorkers = 1;
		autoscalePollInterval = '30s';
		autoscaleBootTimeout = '';
		scaleUp = [];
		scaleDown = [];
		instanceGpu = '';
		instanceImage = '';
		instanceDiskGb = undefined;
		instanceMaxPrice = undefined;
		instanceMinRamGb = undefined;
		instanceMinVcpu = undefined;
		alertsMuted = false;
		alertRules = [];
		preservedLimits = undefined;
		error = '';
	}

	$effect(() => {
		if (open) {
			loadVaults();
			loadProviders();
			if (config && mode === 'edit') {
				const dotIndex = config.name.lastIndexOf('.');
				if (dotIndex > 0) {
					queueName = config.name.substring(0, dotIndex);
					const subName = config.name.substring(dotIndex + 1);
					if (subName !== 'default') {
						subQueues = [{ name: subName, priority: config.priority }];
					}
				} else {
					queueName = config.name;
				}
				if (config.deployment) {
					gitUrl = config.deployment.git_url || '';
					branch = config.deployment.branch || 'main';
					codePath = config.deployment.code_path || '';
					startupCmd = config.deployment.startup_cmd || '';
					deploymentMode = config.deployment.mode || 'long_running';
					startupTimeout = config.deployment.startup_timeout_secs || 60;
					redisStorage = config.deployment.redis_storage || false;
					selectedVaults = new Set(config.deployment.vaults || []);
					if (config.deployment.git_token?.startsWith('vault://')) {
						const ref = config.deployment.git_token.substring(8);
						const parts = ref.split('/');
						if (parts.length >= 2) {
							gitTokenVault = parts[0];
							gitTokenKey = parts.slice(1).join('/');
							loadVaultEntries(gitTokenVault);
						}
					}
					const as = config.deployment.autoscale;
					if (as) {
						autoscaleEnabled = as.enabled ?? false;
						autoscaleProvider = as.provider || '';
						autoscaleMinWorkers = as.min_workers ?? 0;
						autoscaleMaxWorkers = as.max_workers ?? 1;
						autoscalePollInterval = as.poll_interval || '30s';
						autoscaleBootTimeout = as.boot_timeout || '';
						scaleUp = (as.scale_up || []).map((t) => ({ ...t }));
						scaleDown = (as.scale_down || []).map((t) => ({ ...t }));
						instanceGpu = as.instance?.gpu || '';
						instanceImage = as.instance?.image || '';
						instanceDiskGb = as.instance?.disk_gb;
						instanceMaxPrice = as.instance?.max_price_per_hour;
						instanceMinRamGb = as.instance?.min_ram_gb;
						instanceMinVcpu = as.instance?.min_vcpu;
					}
					alertsMuted = config.deployment.alerts?.disabled ?? false;
					alertRules = (config.deployment.alerts?.rules || []).map((r) => ({ ...r }));
					preservedLimits = config.deployment.limits;
				}
			}
		} else {
			resetForm();
		}
	});

	$effect(() => {
		if (gitTokenVault) {
			loadVaultEntries(gitTokenVault);
		} else {
			selectedVaultEntries = [];
			gitTokenKey = '';
		}
	});
</script>

<Modal
	bind:open
	size="lg"
	title={mode === 'create' ? 'Create queue' : 'Edit queue configuration'}
	onclose={() => oncancel?.()}
>
	<form id={formId} class="space-y-5" onsubmit={(e) => { e.preventDefault(); handleSubmit(); }}>
		{#if mode === 'create'}
			<Segmented
				name="createType"
				label="What to create"
				items={[
					{ value: 'queue', label: 'New Queue' },
					{ value: 'subqueue', label: 'Add Subqueue', disabled: parentQueues().length === 0 }
				]}
				bind:value={createType}
			/>
		{/if}

		{#if mode === 'create' && createType === 'subqueue'}
			<div class="space-y-4">
				<Field label="Parent Queue" for="{uid}-parent">
					<select id="{uid}-parent" bind:value={selectedParentQueue} class="select w-full" disabled={loading}>
						<option value="">Select a queue...</option>
						{#each parentQueues() as parent (parent)}
							<option value={parent}>{parent}</option>
						{/each}
					</select>
				</Field>

				<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
					<Field label="Subqueue Name" for="{uid}-subname">
						<div class="flex items-center gap-2">
							<span class="shrink-0 text-ui text-fg-subtle">{selectedParentQueue || '...'}.</span>
							<input id="{uid}-subname" type="text" bind:value={subqueueName} placeholder="high" class="input min-w-0 flex-1" disabled={loading} />
						</div>
					</Field>
					<Field label="Priority" for="{uid}-subprio">
						<input id="{uid}-subprio" type="number" bind:value={subqueuePriority} min="1" class="input w-full" disabled={loading} />
					</Field>
				</div>
				<p class="text-ui text-fg-subtle">Higher priority subqueues are processed first.</p>
			</div>
		{:else}
			<!-- Queue name + sub-queues -->
			<div class="space-y-3">
				<div class="flex items-end gap-3">
					<Field label="Queue Name" for="{uid}-name" class="flex-1 min-w-0">
						<input id="{uid}-name" type="text" bind:value={queueName} placeholder="inference" class="input w-full" disabled={loading || mode === 'edit'} />
					</Field>
					<Button variant="secondary" icon={Plus} onclick={addSubQueue} disabled={loading}>Add Subqueue</Button>
				</div>

				{#if subQueues.length === 0}
					<p class="text-ui text-fg-subtle">
						No subqueues defined. A <code class="text-fg-muted">.default</code> subqueue will be created automatically.
					</p>
				{:else}
					<div class="space-y-2">
						{#each subQueues as sq, index (index)}
							<div class="flex items-center gap-2">
								<span class="shrink-0 text-ui text-fg-subtle">{queueName || '...'}.</span>
								<input type="text" bind:value={sq.name} placeholder="high" aria-label="Subqueue name" class="input min-w-0 flex-1" disabled={loading} />
								<span class="shrink-0 text-xs text-fg-subtle">Priority</span>
								<input type="number" bind:value={sq.priority} min="1" aria-label="Subqueue priority" class="input w-16 shrink-0" disabled={loading} />
								<IconButton icon={X} label="Remove subqueue" danger onclick={() => removeSubQueue(index)} disabled={loading} />
							</div>
						{/each}
					</div>
				{/if}
			</div>

			<!-- Deployment -->
			<section class="space-y-4 border-t border-line-subtle pt-5">
				<div>
					<h3 class="text-sm font-semibold text-fg">Deployment Configuration</h3>
					<p class="mt-0.5 text-ui text-fg-subtle">Configure Git-based code deployment for workers</p>
				</div>

				<Field label="Git URL" for="{uid}-git">
					<input id="{uid}-git" type="text" bind:value={gitUrl} placeholder="https://github.com/org/repo.git" class="input w-full font-mono" disabled={loading} />
				</Field>

				<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
					<Field label="Branch" for="{uid}-branch">
						<input id="{uid}-branch" type="text" bind:value={branch} placeholder="main" class="input w-full" disabled={loading} />
					</Field>
					<Field label="Code Path" for="{uid}-path" hint="Optional">
						<input id="{uid}-path" type="text" bind:value={codePath} placeholder="./src" class="input w-full" disabled={loading} />
					</Field>
				</div>

				<Field label="Startup Command" for="{uid}-cmd">
					<input id="{uid}-cmd" type="text" bind:value={startupCmd} placeholder="python main.py" class="input w-full font-mono" disabled={loading} />
				</Field>

				<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
					<Field label="Mode" for="{uid}-mode">
						<select id="{uid}-mode" bind:value={deploymentMode} class="select w-full" disabled={loading}>
							<option value="long_running">Long Running</option>
							<option value="one_shot">One Shot</option>
						</select>
					</Field>
					<Field label="Startup Timeout (sec)" for="{uid}-timeout">
						<input id="{uid}-timeout" type="number" bind:value={startupTimeout} min="1" class="input w-full" disabled={loading} />
					</Field>
				</div>

				<Toggle bind:checked={redisStorage} label="Redis Storage" disabled={loading} />

				<div class="space-y-1.5">
					<span class="text-ui font-medium text-fg-muted">Vaults</span>
					{#if loadingVaults}
						<p class="text-ui text-fg-subtle">Loading vaults...</p>
					{:else if availableVaults.length === 0}
						<p class="text-ui text-fg-subtle">No vaults available</p>
					{:else}
						<div class="grid grid-cols-1 sm:grid-cols-2 gap-1 max-h-32 overflow-y-auto rounded-md border border-line-subtle p-2">
							{#each availableVaults as vault (vault.name)}
								<label class="flex items-center gap-2 cursor-pointer rounded px-1.5 py-1 hover:bg-hover">
									<input type="checkbox" checked={selectedVaults.has(vault.name)} onchange={() => toggleVault(vault.name)} class="checkbox" disabled={loading} />
									<span class="text-ui text-fg truncate">{vault.name}</span>
									<span class="text-xs text-fg-subtle tabular-nums">({vault.entry_count})</span>
								</label>
							{/each}
						</div>
					{/if}
				</div>

				<Field
					label="Git Token (from vault)"
					for="{uid}-tokvault"
					hint={gitTokenVault && gitTokenKey ? `Reference: vault://${gitTokenVault}/${gitTokenKey}` : undefined}
				>
					<div class="grid grid-cols-2 gap-2">
						<select id="{uid}-tokvault" bind:value={gitTokenVault} class="select w-full" disabled={loading || availableVaults.length === 0}>
							<option value="">Select vault...</option>
							{#each availableVaults as vault (vault.name)}
								<option value={vault.name}>{vault.name}</option>
							{/each}
						</select>
						<select bind:value={gitTokenKey} aria-label="Git token key" class="select w-full" disabled={loading || !gitTokenVault || selectedVaultEntries.length === 0}>
							<option value="">Select key...</option>
							{#each selectedVaultEntries as key (key)}
								<option value={key}>{key}</option>
							{/each}
						</select>
					</div>
				</Field>
			</section>

			<!-- Autoscaling -->
			<section class="space-y-4 border-t border-line-subtle pt-5">
				<div>
					<h3 class="text-sm font-semibold text-fg">Autoscaling</h3>
					<p class="mt-0.5 text-ui text-fg-subtle">Automatically provision GPU workers for this queue</p>
				</div>
				<Toggle bind:checked={autoscaleEnabled} label="Enabled" disabled={loading} />

				{#if autoscaleEnabled}
					<Field
						label="Provider"
						for="{uid}-provider"
						hint={!loadingProviders && availableProviders.length === 0
							? 'No providers configured. Add one on the Autoscaling page.'
							: undefined}
					>
						{#if loadingProviders}
							<p class="text-ui text-fg-subtle">Loading providers...</p>
						{:else}
							<select id="{uid}-provider" bind:value={autoscaleProvider} class="select w-full" disabled={loading}>
								<option value="">Select provider...</option>
								{#each availableProviders as provider (provider.name)}
									<option value={provider.name}>{provider.name}</option>
								{/each}
							</select>
						{/if}
					</Field>

					<div class="grid grid-cols-2 sm:grid-cols-4 gap-4">
						<Field label="Min Workers" for="{uid}-min">
							<input id="{uid}-min" type="number" bind:value={autoscaleMinWorkers} min="0" class="input w-full" disabled={loading} />
						</Field>
						<Field label="Max Workers" for="{uid}-max">
							<input id="{uid}-max" type="number" bind:value={autoscaleMaxWorkers} min="1" class="input w-full" disabled={loading} />
						</Field>
						<Field label="Poll Interval" for="{uid}-poll">
							<input id="{uid}-poll" type="text" bind:value={autoscalePollInterval} placeholder="30s" class="input w-full" disabled={loading} />
						</Field>
						<Field label="Boot Timeout" for="{uid}-boot">
							<input id="{uid}-boot" type="text" bind:value={autoscaleBootTimeout} placeholder="20m" class="input w-full" disabled={loading} />
						</Field>
					</div>

					<!-- Scale Up Triggers -->
					<div class="space-y-2">
						<div class="flex items-center justify-between gap-2">
							<span class="text-xs font-medium text-fg-subtle">Scale Up Triggers</span>
							<Button variant="ghost" size="sm" icon={Plus} onclick={addScaleUp} disabled={loading}>Add Trigger</Button>
						</div>
						{#if scaleUp.length === 0}
							<p class="text-ui text-fg-subtle">No scale-up triggers defined.</p>
						{:else}
							<div class="divide-y divide-line-subtle rounded-md border border-line-subtle">
								{#each scaleUp as trigger, index (index)}
									<div class="flex flex-wrap items-end gap-2 p-3">
										<Field label="Type" for="{uid}-up{index}-type" class="flex-1 min-w-[140px]">
											<select id="{uid}-up{index}-type" bind:value={trigger.trigger} class="select w-full" disabled={loading}>
												{#each scaleUpTypes as t (t)}
													<option value={t}>{t}</option>
												{/each}
											</select>
										</Field>
										{#if trigger.trigger === 'queue_depth'}
											<Field label="Threshold" for="{uid}-up{index}-thr" class="w-28">
												<input id="{uid}-up{index}-thr" type="number" bind:value={trigger.threshold} min="0" class="input w-full" disabled={loading} />
											</Field>
										{:else if trigger.trigger === 'latency'}
											<Field label="Max Latency" for="{uid}-up{index}-lat" class="w-28">
												<input id="{uid}-up{index}-lat" type="text" bind:value={trigger.max_latency} placeholder="5m" class="input w-full" disabled={loading} />
											</Field>
										{:else if trigger.trigger === 'schedule'}
											<Field label="Cron" for="{uid}-up{index}-cron" class="flex-1 min-w-[120px]">
												<input id="{uid}-up{index}-cron" type="text" bind:value={trigger.cron} placeholder="0 9 * * *" class="input w-full font-mono" disabled={loading} />
											</Field>
											<Field label="Workers" for="{uid}-up{index}-workers" class="w-24">
												<input id="{uid}-up{index}-workers" type="number" bind:value={trigger.workers} min="0" class="input w-full" disabled={loading} />
											</Field>
											<Field label="Hold for" for="{uid}-up{index}-hold" class="w-24">
												<input id="{uid}-up{index}-hold" type="text" bind:value={trigger.duration} placeholder="8h" class="input w-full" disabled={loading} />
											</Field>
										{/if}
										{#if watchesSubQueues(trigger.trigger)}
											<Field label="Sub-queues (optional)" for="{uid}-up{index}-subs" class="flex-1 min-w-[140px]">
												<input
													id="{uid}-up{index}-subs"
													type="text"
													bind:value={
														() => (trigger.sub_queues ?? []).join(', '),
														(v) => (trigger.sub_queues = parseSubQueueList(v))
													}
													placeholder="all"
													class="input w-full"
													disabled={loading}
												/>
											</Field>
										{/if}
										<div class="flex h-9 items-center">
											<IconButton icon={X} label="Remove trigger" danger onclick={() => removeScaleUp(index)} disabled={loading} />
										</div>
									</div>
								{/each}
							</div>
						{/if}
					</div>

					<!-- Scale Down Triggers -->
					<div class="space-y-2">
						<div class="flex items-center justify-between gap-2">
							<span class="text-xs font-medium text-fg-subtle">Scale Down Triggers</span>
							<Button variant="ghost" size="sm" icon={Plus} onclick={addScaleDown} disabled={loading}>Add Trigger</Button>
						</div>
						{#if scaleDown.length === 0}
							<p class="text-ui text-fg-subtle">No scale-down triggers defined.</p>
						{:else}
							<div class="divide-y divide-line-subtle rounded-md border border-line-subtle">
								{#each scaleDown as trigger, index (index)}
									<div class="flex flex-wrap items-end gap-2 p-3">
										<Field label="Type" for="{uid}-down{index}-type" class="flex-1 min-w-[140px]">
											<select id="{uid}-down{index}-type" bind:value={trigger.trigger} class="select w-full" disabled={loading}>
												{#each scaleDownTypes as t (t)}
													<option value={t}>{t}</option>
												{/each}
											</select>
										</Field>
										{#if trigger.trigger === 'schedule'}
											<Field label="Cron" for="{uid}-down{index}-cron" class="flex-1 min-w-[120px]">
												<input id="{uid}-down{index}-cron" type="text" bind:value={trigger.cron} placeholder="0 18 * * *" class="input w-full font-mono" disabled={loading} />
											</Field>
										{:else if trigger.trigger === 'idle'}
											<Field label="Timeout" for="{uid}-down{index}-timeout" class="w-28">
												<input id="{uid}-down{index}-timeout" type="text" bind:value={trigger.timeout} placeholder="10m" class="input w-full" disabled={loading} />
											</Field>
										{/if}
										<div class="flex h-9 items-center">
											<IconButton icon={X} label="Remove trigger" danger onclick={() => removeScaleDown(index)} disabled={loading} />
										</div>
									</div>
								{/each}
							</div>
						{/if}
					</div>

					<!-- Instance Spec -->
					<div class="space-y-2">
						<span class="text-xs font-medium text-fg-subtle">Instance Spec</span>
						<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
							<Field label="GPU" for="{uid}-gpu" hint="Optional">
								<input id="{uid}-gpu" type="text" bind:value={instanceGpu} placeholder="A100" class="input w-full" disabled={loading} />
							</Field>
							<Field label="Image" for="{uid}-image" hint="Optional">
								<input id="{uid}-image" type="text" bind:value={instanceImage} placeholder="nvidia/cuda:12" class="input w-full font-mono" disabled={loading} />
							</Field>
							<Field label="Disk (GB)" for="{uid}-disk" hint="Optional">
								<input id="{uid}-disk" type="number" bind:value={instanceDiskGb} min="0" class="input w-full" disabled={loading} />
							</Field>
							<Field label="Max Price/hr" for="{uid}-price" hint="Optional">
								<input id="{uid}-price" type="number" step="0.01" bind:value={instanceMaxPrice} min="0" class="input w-full" disabled={loading} />
							</Field>
							<Field label="Min RAM (GB)" for="{uid}-ram" hint="Optional">
								<input id="{uid}-ram" type="number" bind:value={instanceMinRamGb} min="0" class="input w-full" disabled={loading} />
							</Field>
							<Field label="Min vCPUs" for="{uid}-vcpu" hint="Optional">
								<input id="{uid}-vcpu" type="number" bind:value={instanceMinVcpu} min="0" class="input w-full" disabled={loading} />
							</Field>
						</div>
					</div>
				{/if}
			</section>

			<!-- Alerts -->
			<section class="space-y-4 border-t border-line-subtle pt-5">
				<div>
					<h3 class="text-sm font-semibold text-fg">Alerts</h3>
					<p class="mt-0.5 text-ui text-fg-subtle">Override the global alert rules for this queue</p>
				</div>
				<Toggle bind:checked={alertsMuted} label="Mute" disabled={loading} />
				{#if !alertsMuted}
					<AlertRulesEditor bind:rules={alertRules} override disabled={loading} />
				{/if}
			</section>
		{/if}

		{#if error}
			<Callout>
				{error}
			</Callout>
		{/if}
	</form>

	{#snippet footer()}
		<Button variant="ghost" onclick={handleCancel} disabled={loading}>Cancel</Button>
		<Button type="submit" form={formId} variant="primary" {loading}>
			{#if loading}
				Saving...
			{:else if mode === 'create' && createType === 'subqueue'}
				Add Subqueue
			{:else if mode === 'create'}
				Create Queue
			{:else}
				Update Queue
			{/if}
		</Button>
	{/snippet}
</Modal>
