<script lang="ts">
	import { SegmentedControl } from '@skeletonlabs/skeleton-svelte';
	import type {
		QueueConfigDetail,
		DeploymentConfig,
		VaultSummary,
		AutoscaleConfig,
		AutoscaleProvider,
		ScaleTrigger,
		ScaleTriggerType
	} from '$lib/api/types';
	import { getVaults, getVault, getAutoscaleProviders } from '$lib/api/client';

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
	let scaleUp = $state<ScaleTrigger[]>([]);
	let scaleDown = $state<ScaleTrigger[]>([]);
	let instanceGpu = $state('');
	let instanceImage = $state('');
	let instanceDiskGb = $state<number | undefined>(undefined);
	let instanceMaxPrice = $state<number | undefined>(undefined);
	let availableProviders = $state<AutoscaleProvider[]>([]);
	let loadingProviders = $state(false);

	const scaleUpTypes: ScaleTriggerType[] = ['no_workers', 'queue_depth', 'schedule'];
	const scaleDownTypes: ScaleTriggerType[] = ['idle', 'queue_depth', 'schedule'];

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
					git_token: gitTokenRef || undefined
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
				if (t.trigger === 'queue_depth' && (t.threshold === undefined || t.threshold === null)) {
					error = 'queue_depth scale-down triggers require a threshold';
					return;
				}
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
				if (t.trigger === 'schedule') {
					out.cron = t.cron?.trim();
					if (t.workers !== undefined && t.workers !== null) out.workers = t.workers;
				}
				if (t.trigger === 'idle') out.timeout = t.timeout?.trim();
				return out;
			};

			const autoscale: AutoscaleConfig = {
				enabled: true,
				provider: autoscaleProvider,
				min_workers: autoscaleMinWorkers,
				max_workers: autoscaleMaxWorkers,
				poll_interval: autoscalePollInterval.trim() || '30s',
				scale_up: scaleUp.map(cleanTrigger),
				scale_down: scaleDown.map(cleanTrigger),
				instance: {
					gpu: instanceGpu.trim() || undefined,
					image: instanceImage.trim() || undefined,
					disk_gb: instanceDiskGb,
					max_price_per_hour: instanceMaxPrice
				}
			};
			deployment.autoscale = autoscale;
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

	function handleBackdropClick(e: MouseEvent) {
		if (e.target === e.currentTarget) {
			handleCancel();
		}
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
		scaleUp = [];
		scaleDown = [];
		instanceGpu = '';
		instanceImage = '';
		instanceDiskGb = undefined;
		instanceMaxPrice = undefined;
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
						scaleUp = (as.scale_up || []).map((t) => ({ ...t }));
						scaleDown = (as.scale_down || []).map((t) => ({ ...t }));
						instanceGpu = as.instance?.gpu || '';
						instanceImage = as.instance?.image || '';
						instanceDiskGb = as.instance?.disk_gb;
						instanceMaxPrice = as.instance?.max_price_per_hour;
					}
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

{#if open}
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<div
		class="fixed inset-0 z-50 bg-surface-950/70 backdrop-blur-sm flex justify-center items-center p-4 overflow-y-auto"
		onclick={handleBackdropClick}
	>
		<div class="card preset-outlined-surface-200-800 bg-surface-100-900 ring-1 ring-surface-300 dark:ring-surface-600 w-full max-w-2xl p-6 shadow-xl max-h-[90vh] overflow-y-auto">
			<h2 class="h4 mb-4">
				{mode === 'create' ? 'Create Queue' : 'Edit Queue Configuration'}
			</h2>

			<form onsubmit={(e) => { e.preventDefault(); handleSubmit(); }}>
				<div class="space-y-4">
					{#if mode === 'create'}
						<SegmentedControl
							name="createType"
							value={createType}
							onValueChange={(e) => createType = e.value as 'queue' | 'subqueue'}
						>
							<SegmentedControl.Item value="queue">New Queue</SegmentedControl.Item>
							<SegmentedControl.Item value="subqueue" disabled={parentQueues().length === 0}>Add Subqueue</SegmentedControl.Item>
						</SegmentedControl>
					{/if}

					{#if mode === 'create' && createType === 'subqueue'}
						<div class="space-y-4">
							<label class="label">
								<span class="label-text">Parent Queue</span>
								<select bind:value={selectedParentQueue} class="select" disabled={loading}>
									<option value="">Select a queue...</option>
									{#each parentQueues() as parent}
										<option value={parent}>{parent}</option>
									{/each}
								</select>
							</label>

							<div class="grid grid-cols-2 gap-4">
								<label class="label">
									<span class="label-text">Subqueue Name</span>
									<div class="input-group grid-cols-[auto_1fr]">
										<span class="ig-cell">{selectedParentQueue || '...'}.</span>
										<input type="text" bind:value={subqueueName} placeholder="high" class="ig-input" disabled={loading} />
									</div>
								</label>
								<label class="label">
									<span class="label-text">Priority</span>
									<input type="number" bind:value={subqueuePriority} min="1" class="input" disabled={loading} />
								</label>
							</div>
							<p class="text-sm text-surface-500">
								Higher priority subqueues are processed first.
							</p>
						</div>
					{:else}
						<div class="space-y-4">
							<div class="flex items-end gap-4">
								<label class="label flex-1">
									<span class="label-text">Queue Name</span>
									<input type="text" bind:value={queueName} placeholder="inference" class="input" disabled={loading || mode === 'edit'} />
								</label>
								<button type="button" class="btn preset-tonal" onclick={addSubQueue} disabled={loading}>
									+ Add Subqueue
								</button>
							</div>

							{#if subQueues.length === 0}
								<p class="text-sm text-surface-500 py-2">
									No subqueues defined. A <code>.default</code> subqueue will be created automatically.
								</p>
							{:else}
								<div class="card preset-outlined p-3 space-y-2">
									{#each subQueues as sq, index}
										<div class="flex items-center gap-2">
											<span class="text-surface-500 text-sm">{queueName || '...'}.</span>
											<input type="text" bind:value={sq.name} placeholder="high" class="input flex-1" disabled={loading} />
											<span class="text-xs text-surface-500">Priority:</span>
											<input type="number" bind:value={sq.priority} min="1" class="input w-16" disabled={loading} />
											<button type="button" class="btn-icon preset-tonal-error" onclick={() => removeSubQueue(index)} disabled={loading}>
												<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
													<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
												</svg>
											</button>
										</div>
									{/each}
								</div>
							{/if}

							<hr class="hr" />

							<div>
								<h5 class="h5 mb-1">Deployment Configuration</h5>
								<p class="text-sm text-surface-500 mb-4">Configure Git-based code deployment for workers</p>
							</div>

							<div class="space-y-4">
								<label class="label">
									<span class="label-text">Git URL</span>
									<input type="text" bind:value={gitUrl} placeholder="https://github.com/org/repo.git" class="input font-mono text-sm" disabled={loading} />
								</label>

								<div class="grid grid-cols-2 gap-4">
									<label class="label">
										<span class="label-text">Branch</span>
										<input type="text" bind:value={branch} placeholder="main" class="input" disabled={loading} />
									</label>
									<label class="label">
										<span class="label-text">Code Path <span class="text-surface-500">(optional)</span></span>
										<input type="text" bind:value={codePath} placeholder="./src" class="input" disabled={loading} />
									</label>
								</div>

								<label class="label">
									<span class="label-text">Startup Command</span>
									<input type="text" bind:value={startupCmd} placeholder="python main.py" class="input font-mono text-sm" disabled={loading} />
								</label>

								<div class="grid grid-cols-2 gap-4">
									<label class="label">
										<span class="label-text">Mode</span>
										<select bind:value={deploymentMode} class="select" disabled={loading}>
											<option value="long_running">Long Running</option>
											<option value="one_shot">One Shot</option>
										</select>
									</label>
									<label class="label">
										<span class="label-text">Startup Timeout (sec)</span>
										<input type="number" bind:value={startupTimeout} min="1" class="input" disabled={loading} />
									</label>
								</div>

								<label class="flex items-center gap-3 cursor-pointer">
									<input type="checkbox" bind:checked={redisStorage} class="checkbox" disabled={loading} />
									<span class="text-sm">Redis Storage</span>
								</label>

								<div>
									<span class="label-text">Vaults</span>
									{#if loadingVaults}
										<p class="text-sm text-surface-500">Loading vaults...</p>
									{:else if availableVaults.length === 0}
										<p class="text-sm text-surface-500">No vaults available</p>
									{:else}
										<div class="card preset-outlined p-2 max-h-32 overflow-y-auto grid grid-cols-2 gap-2 mt-1">
											{#each availableVaults as vault}
												<label class="flex items-center gap-2 cursor-pointer p-1 rounded hover:preset-tonal">
													<input type="checkbox" checked={selectedVaults.has(vault.name)} onchange={() => toggleVault(vault.name)} class="checkbox" disabled={loading} />
													<span class="text-sm">{vault.name}</span>
													<span class="text-xs text-surface-400">({vault.entry_count})</span>
												</label>
											{/each}
										</div>
									{/if}
								</div>

								<div>
									<span class="label-text">Git Token (from vault)</span>
									<div class="grid grid-cols-2 gap-2 mt-1">
										<select bind:value={gitTokenVault} class="select" disabled={loading || availableVaults.length === 0}>
											<option value="">Select vault...</option>
											{#each availableVaults as vault}
												<option value={vault.name}>{vault.name}</option>
											{/each}
										</select>
										<select bind:value={gitTokenKey} class="select" disabled={loading || !gitTokenVault || selectedVaultEntries.length === 0}>
											<option value="">Select key...</option>
											{#each selectedVaultEntries as key}
												<option value={key}>{key}</option>
											{/each}
										</select>
									</div>
									{#if gitTokenVault && gitTokenKey}
										<p class="text-xs text-surface-500 mt-1">
											Reference: <code>vault://{gitTokenVault}/{gitTokenKey}</code>
										</p>
									{/if}
								</div>
							</div>

							<hr class="hr" />

							<div>
								<div class="flex items-center justify-between">
									<div>
										<h5 class="h5 mb-1">Autoscaling</h5>
										<p class="text-sm text-surface-500">Automatically provision GPU workers for this queue</p>
									</div>
									<label class="flex items-center gap-3 cursor-pointer">
										<input type="checkbox" bind:checked={autoscaleEnabled} class="checkbox" disabled={loading} />
										<span class="text-sm">Enabled</span>
									</label>
								</div>
							</div>

							{#if autoscaleEnabled}
								<div class="space-y-4">
									<label class="label">
										<span class="label-text">Provider</span>
										{#if loadingProviders}
											<p class="text-sm text-surface-500">Loading providers...</p>
										{:else}
											<select bind:value={autoscaleProvider} class="select" disabled={loading}>
												<option value="">Select provider...</option>
												{#each availableProviders as provider}
													<option value={provider.name}>{provider.name}</option>
												{/each}
											</select>
											{#if availableProviders.length === 0}
												<span class="label-text text-surface-500">No providers configured. Add one on the Autoscaling page.</span>
											{/if}
										{/if}
									</label>

									<div class="grid grid-cols-3 gap-4">
										<label class="label">
											<span class="label-text">Min Workers</span>
											<input type="number" bind:value={autoscaleMinWorkers} min="0" class="input" disabled={loading} />
										</label>
										<label class="label">
											<span class="label-text">Max Workers</span>
											<input type="number" bind:value={autoscaleMaxWorkers} min="1" class="input" disabled={loading} />
										</label>
										<label class="label">
											<span class="label-text">Poll Interval</span>
											<input type="text" bind:value={autoscalePollInterval} placeholder="30s" class="input" disabled={loading} />
										</label>
									</div>

									<!-- Scale Up Triggers -->
									<div>
										<div class="flex items-center justify-between mb-2">
											<span class="label-text">Scale Up Triggers</span>
											<button type="button" class="btn btn-sm preset-tonal" onclick={addScaleUp} disabled={loading}>
												+ Add Trigger
											</button>
										</div>
										{#if scaleUp.length === 0}
											<p class="text-sm text-surface-500">No scale-up triggers defined.</p>
										{:else}
											<div class="card preset-outlined p-3 space-y-3">
												{#each scaleUp as trigger, index}
													<div class="flex flex-wrap items-end gap-2">
														<label class="label flex-1 min-w-[140px]">
															<span class="label-text text-xs">Type</span>
															<select bind:value={trigger.trigger} class="select" disabled={loading}>
																{#each scaleUpTypes as t}
																	<option value={t}>{t}</option>
																{/each}
															</select>
														</label>
														{#if trigger.trigger === 'queue_depth'}
															<label class="label w-28">
																<span class="label-text text-xs">Threshold</span>
																<input type="number" bind:value={trigger.threshold} min="0" class="input" disabled={loading} />
															</label>
														{:else if trigger.trigger === 'schedule'}
															<label class="label flex-1 min-w-[120px]">
																<span class="label-text text-xs">Cron</span>
																<input type="text" bind:value={trigger.cron} placeholder="0 9 * * *" class="input font-mono text-sm" disabled={loading} />
															</label>
															<label class="label w-24">
																<span class="label-text text-xs">Workers</span>
																<input type="number" bind:value={trigger.workers} min="0" class="input" disabled={loading} />
															</label>
														{/if}
														<button type="button" class="btn-icon preset-tonal-error" onclick={() => removeScaleUp(index)} disabled={loading}>
															<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
																<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
															</svg>
														</button>
													</div>
												{/each}
											</div>
										{/if}
									</div>

									<!-- Scale Down Triggers -->
									<div>
										<div class="flex items-center justify-between mb-2">
											<span class="label-text">Scale Down Triggers</span>
											<button type="button" class="btn btn-sm preset-tonal" onclick={addScaleDown} disabled={loading}>
												+ Add Trigger
											</button>
										</div>
										{#if scaleDown.length === 0}
											<p class="text-sm text-surface-500">No scale-down triggers defined.</p>
										{:else}
											<div class="card preset-outlined p-3 space-y-3">
												{#each scaleDown as trigger, index}
													<div class="flex flex-wrap items-end gap-2">
														<label class="label flex-1 min-w-[140px]">
															<span class="label-text text-xs">Type</span>
															<select bind:value={trigger.trigger} class="select" disabled={loading}>
																{#each scaleDownTypes as t}
																	<option value={t}>{t}</option>
																{/each}
															</select>
														</label>
														{#if trigger.trigger === 'queue_depth'}
															<label class="label w-28">
																<span class="label-text text-xs">Threshold</span>
																<input type="number" bind:value={trigger.threshold} min="0" class="input" disabled={loading} />
															</label>
														{:else if trigger.trigger === 'schedule'}
															<label class="label flex-1 min-w-[120px]">
																<span class="label-text text-xs">Cron</span>
																<input type="text" bind:value={trigger.cron} placeholder="0 18 * * *" class="input font-mono text-sm" disabled={loading} />
															</label>
														{:else if trigger.trigger === 'idle'}
															<label class="label w-28">
																<span class="label-text text-xs">Timeout</span>
																<input type="text" bind:value={trigger.timeout} placeholder="10m" class="input" disabled={loading} />
															</label>
														{/if}
														<button type="button" class="btn-icon preset-tonal-error" onclick={() => removeScaleDown(index)} disabled={loading}>
															<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
																<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
															</svg>
														</button>
													</div>
												{/each}
											</div>
										{/if}
									</div>

									<!-- Instance Spec -->
									<div>
										<span class="label-text">Instance Spec</span>
										<div class="grid grid-cols-2 gap-4 mt-1">
											<label class="label">
												<span class="label-text text-xs">GPU <span class="text-surface-500">(optional)</span></span>
												<input type="text" bind:value={instanceGpu} placeholder="A100" class="input" disabled={loading} />
											</label>
											<label class="label">
												<span class="label-text text-xs">Image <span class="text-surface-500">(optional)</span></span>
												<input type="text" bind:value={instanceImage} placeholder="nvidia/cuda:12" class="input font-mono text-sm" disabled={loading} />
											</label>
											<label class="label">
												<span class="label-text text-xs">Disk (GB) <span class="text-surface-500">(optional)</span></span>
												<input type="number" bind:value={instanceDiskGb} min="0" class="input" disabled={loading} />
											</label>
											<label class="label">
												<span class="label-text text-xs">Max Price/hr <span class="text-surface-500">(optional)</span></span>
												<input type="number" step="0.01" bind:value={instanceMaxPrice} min="0" class="input" disabled={loading} />
											</label>
										</div>
									</div>
								</div>
							{/if}
						</div>
					{/if}

					{#if error}
						<aside class="alert preset-filled-error-500">
							<p>{error}</p>
						</aside>
					{/if}
				</div>

				<footer class="flex justify-end gap-2 mt-6">
					<button type="button" class="btn preset-tonal" onclick={handleCancel} disabled={loading}>
						Cancel
					</button>
					<button type="submit" class="btn preset-filled" disabled={loading}>
						{#if loading}
							Saving...
						{:else if mode === 'create' && createType === 'subqueue'}
							Add Subqueue
						{:else if mode === 'create'}
							Create Queue
						{:else}
							Update Queue
						{/if}
					</button>
				</footer>
			</form>
		</div>
	</div>
{/if}
