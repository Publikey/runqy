<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { base } from '$app/paths';
	import { settings as uiSettings } from '$lib/stores/settings';
	import { toaster } from '$lib/stores/toaster';
	import { getAlertsOverview, getAlertSettings, saveAlertSettings, sendTestAlert } from '$lib/api/client';
	import type { AlertsOverview, AlertSettings, AlertRule } from '$lib/api/types';
	import { formatRelativeTime } from '$lib/utils/format';
	import AlertRulesEditor from '$lib/components/AlertRulesEditor.svelte';

	const ruleLabels: Record<string, string> = {
		no_workers: 'No workers',
		latency: 'Latency',
		pending: 'Backlog',
		worker_unhealthy: 'Unhealthy worker',
		autoscale_failure: 'Autoscaling failure'
	};

	const statusBadges: Record<string, string> = {
		firing: 'badge preset-filled-error-500 text-xs',
		repeat: 'badge preset-filled-error-500 text-xs',
		resolved: 'badge preset-filled-success-500 text-xs',
		event: 'badge preset-filled-warning-500 text-xs'
	};

	let overview = $state<AlertsOverview | null>(null);
	let loadError = $state('');
	let refreshing = $state(false);
	let pollInterval: ReturnType<typeof setInterval> | null = null;

	// Settings form
	let form = $state<AlertSettings>(emptySettings());
	let recipientsText = $state('');
	let rules = $state<AlertRule[]>([]);
	let sendResolved = $state(true);
	let saving = $state(false);
	let testing = $state(false);
	let settingsLoaded = $state(false);

	function emptySettings(): AlertSettings {
		return {
			enabled: false,
			provider: 'smtp',
			from: '',
			recipients: [],
			smtp: { host: '', port: undefined, username: '', password: '', tls: 'starttls' },
			sendgrid: { api_key: '' },
			rules: [],
			repeat_interval: '4h'
		};
	}

	async function loadOverview() {
		try {
			overview = await getAlertsOverview(50);
			loadError = '';
		} catch (e) {
			loadError = e instanceof Error ? e.message : 'Failed to load alerts';
		}
	}

	async function loadSettings() {
		try {
			const { settings } = await getAlertSettings();
			form = {
				...emptySettings(),
				...settings,
				smtp: { ...emptySettings().smtp!, ...(settings.smtp ?? {}) },
				sendgrid: { ...emptySettings().sendgrid!, ...(settings.sendgrid ?? {}) }
			};
			recipientsText = (settings.recipients ?? []).join('\n');
			rules = (settings.rules ?? []).map((r) => ({ ...r }));
			sendResolved = settings.send_resolved ?? true;
			settingsLoaded = true;
		} catch (e) {
			toaster.error({ title: 'Error', description: e instanceof Error ? e.message : 'Failed to load alert settings' });
		}
	}

	function buildSettings(): AlertSettings {
		const out: AlertSettings = {
			enabled: form.enabled,
			provider: form.provider,
			from: form.from.trim(),
			recipients: recipientsText
				.split(/[\n,;]+/)
				.map((r) => r.trim())
				.filter(Boolean),
			rules: rules.map((r) => {
				const rule: AlertRule = { type: r.type };
				if (r.for?.trim()) rule.for = r.for.trim();
				if (r.type === 'latency') rule.max_latency = r.max_latency?.trim();
				if (r.type === 'pending') rule.threshold = Number(r.threshold) || 0;
				if (r.type === 'no_workers' && r.when_idle) rule.when_idle = true;
				return rule;
			}),
			repeat_interval: form.repeat_interval?.trim() || undefined,
			send_resolved: sendResolved
		};
		if (form.provider === 'smtp' && form.smtp) {
			out.smtp = {
				host: form.smtp.host.trim(),
				port: form.smtp.port ? Number(form.smtp.port) : undefined,
				username: form.smtp.username?.trim() || undefined,
				password: form.smtp.password || undefined,
				tls: form.smtp.tls || undefined
			};
		}
		if (form.provider === 'sendgrid') {
			out.sendgrid = { api_key: form.sendgrid?.api_key || undefined };
		}
		return out;
	}

	async function handleSave() {
		saving = true;
		try {
			await saveAlertSettings(buildSettings());
			toaster.success({ title: 'Alert settings saved' });
			await Promise.all([loadSettings(), loadOverview()]);
		} catch (e) {
			toaster.error({ title: 'Could not save', description: e instanceof Error ? e.message : 'Failed to save' });
		} finally {
			saving = false;
		}
	}

	async function handleTest() {
		testing = true;
		try {
			await sendTestAlert(buildSettings());
			toaster.success({ title: 'Test email sent', description: 'Check the recipients inbox (and spam folder).' });
		} catch (e) {
			toaster.error({ title: 'Test failed', description: e instanceof Error ? e.message : 'Failed to send' });
		} finally {
			testing = false;
		}
	}

	async function handleRefresh() {
		refreshing = true;
		await loadOverview();
		setTimeout(() => (refreshing = false), 500);
	}

	onMount(() => {
		loadOverview();
		loadSettings();
		pollInterval = setInterval(loadOverview, $uiSettings.pollInterval * 1000);
	});

	onDestroy(() => {
		if (pollInterval) clearInterval(pollInterval);
	});

	let firingCount = $derived(overview?.active.filter((a) => a.firing).length ?? 0);
</script>

<svelte:head>
	<title>Alerts - runqy Monitor</title>
</svelte:head>

<div class="p-4 md:p-6 space-y-8">
	<!-- Header -->
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<h1 class="text-2xl font-bold">Alerts</h1>
			<p class="text-surface-500">
				{#if overview}
					{overview.enabled ? 'Enabled' : 'Disabled'}
					&middot; {firingCount} firing
					{#if overview.status.last_eval}
						&middot; Evaluated {formatRelativeTime(overview.status.last_eval)}
					{/if}
				{:else}
					Loading...
				{/if}
			</p>
		</div>
		<button type="button" class="rq-btn-ghost {refreshing ? 'refresh-spinning' : ''}" onclick={handleRefresh}>
			<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
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

	{#if loadError}
		<div class="card p-4 preset-outlined-error-500">
			<p class="text-error-500">{loadError}</p>
		</div>
	{/if}

	{#if overview?.status.last_error}
		<div class="card p-4 preset-outlined-error-500">
			<p class="text-sm font-semibold text-error-500">Last delivery error</p>
			<p class="text-sm text-surface-300 mt-1 break-words">{overview.status.last_error}</p>
			{#if overview.status.last_error_at}
				<p class="text-xs text-surface-500 mt-1">{formatRelativeTime(overview.status.last_error_at)}</p>
			{/if}
		</div>
	{/if}

	<!-- Active alerts -->
	<section class="space-y-4">
		<h2 class="rq-section-title">Active</h2>
		{#if !overview || overview.active.length === 0}
			<div class="rq-card rq-empty-state">
				<svg class="w-12 h-12" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
				</svg>
				<p class="text-surface-400">No active alerts</p>
			</div>
		{:else}
			<div class="rq-card table-container">
				<table class="rq-table">
					<thead>
						<tr><th>State</th><th>Queue</th><th>Alert</th><th>Since</th><th>Details</th></tr>
					</thead>
					<tbody>
						{#each overview.active as a (a.queue + a.rule)}
							<tr>
								<td>
									{#if a.firing}
										<span class="badge preset-filled-error-500 text-xs">Firing</span>
									{:else}
										<span class="badge preset-outlined-surface-500 text-xs">Pending</span>
									{/if}
								</td>
								<td><a class="anchor" href="{base}/queues/{encodeURIComponent(a.queue)}">{a.queue}</a></td>
								<td>{ruleLabels[a.rule] ?? a.rule}</td>
								<td class="text-surface-400">{formatRelativeTime(a.firing && a.fired_at ? a.fired_at : a.since)}</td>
								<td class="text-surface-400">{a.message}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>

	<!-- Settings -->
	<section class="space-y-4">
		<h2 class="rq-section-title">Settings</h2>
		<div class="rq-card p-5 space-y-6">
			{#if !settingsLoaded}
				<p class="text-surface-500">Loading settings...</p>
			{:else}
				<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
					<div>
						<h3 class="font-semibold">Email notifications</h3>
						<p class="text-sm text-surface-400">
							A top-level <code>alerts</code> block in a queue YAML file replaces these settings on the next config reload.
						</p>
					</div>
					<label class="flex items-center gap-3 cursor-pointer">
						<input type="checkbox" bind:checked={form.enabled} class="checkbox" disabled={saving} />
						<span class="text-sm">Enabled</span>
					</label>
				</div>

				<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
					<label class="label">
						<span class="label-text">Provider</span>
						<select bind:value={form.provider} class="select" disabled={saving}>
							<option value="smtp">SMTP</option>
							<option value="sendgrid">SendGrid</option>
						</select>
					</label>
					<label class="label">
						<span class="label-text">From</span>
						<input type="text" bind:value={form.from} placeholder="runqy <alerts@example.com>" class="input" disabled={saving} />
					</label>
					<label class="label md:col-span-2">
						<span class="label-text">Recipients <span class="text-surface-500">(one per line)</span></span>
						<textarea bind:value={recipientsText} rows="3" placeholder="ops@example.com" class="textarea" disabled={saving}></textarea>
					</label>
				</div>

				{#if form.provider === 'smtp' && form.smtp}
					<div class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-4 gap-4">
						<label class="label sm:col-span-2">
							<span class="label-text">SMTP host</span>
							<input type="text" bind:value={form.smtp.host} placeholder="smtp.example.com" class="input" disabled={saving} />
						</label>
						<label class="label">
							<span class="label-text">Security</span>
							<select bind:value={form.smtp.tls} class="select" disabled={saving}>
								<option value="starttls">STARTTLS (587)</option>
								<option value="tls">TLS (465)</option>
								<option value="none">None (25)</option>
							</select>
						</label>
						<label class="label">
							<span class="label-text">Port <span class="text-surface-500">(optional)</span></span>
							<input type="number" bind:value={form.smtp.port} min="1" max="65535" class="input" disabled={saving} />
						</label>
						<label class="label sm:col-span-2">
							<span class="label-text">Username</span>
							<input type="text" bind:value={form.smtp.username} autocomplete="off" class="input" disabled={saving} />
						</label>
						<label class="label sm:col-span-2">
							<span class="label-text">Password</span>
							<input type="password" bind:value={form.smtp.password} autocomplete="new-password" placeholder="vault://vault/key, or empty to use RUNQY_SMTP_PASSWORD" class="input" disabled={saving} />
						</label>
					</div>
				{:else if form.provider === 'sendgrid' && form.sendgrid}
					<label class="label">
						<span class="label-text">SendGrid API key</span>
						<input type="password" bind:value={form.sendgrid.api_key} autocomplete="new-password" placeholder="vault://vault/key, or empty to use RUNQY_SENDGRID_API_KEY" class="input" disabled={saving} />
					</label>
				{/if}

				<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
					<label class="label">
						<span class="label-text">Repeat while firing</span>
						<input type="text" bind:value={form.repeat_interval} placeholder="4h (0 = once)" class="input" disabled={saving} />
					</label>
					<label class="flex items-center gap-3 cursor-pointer sm:mt-6">
						<input type="checkbox" bind:checked={sendResolved} class="checkbox" disabled={saving} />
						<span class="text-sm">Notify when an alert resolves</span>
					</label>
				</div>

				<div class="space-y-2">
					<h3 class="font-semibold">Default rules</h3>
					<p class="text-sm text-surface-400">Applied to every queue. A queue can override or turn off a rule in its configuration.</p>
					<AlertRulesEditor bind:rules disabled={saving} />
				</div>

				<div class="flex flex-wrap justify-end gap-2 pt-2">
					<button type="button" class="rq-btn-ghost" onclick={handleTest} disabled={testing || saving}>
						{testing ? 'Sending...' : 'Send test email'}
					</button>
					<button type="button" class="rq-btn-primary" onclick={handleSave} disabled={saving}>
						{saving ? 'Saving...' : 'Save settings'}
					</button>
				</div>
			{/if}
		</div>
	</section>

	<!-- History -->
	<section class="space-y-4">
		<h2 class="rq-section-title">Recent notifications</h2>
		{#if !overview || overview.history.length === 0}
			<div class="rq-card rq-empty-state">
				<p class="text-surface-400">No notifications sent yet</p>
			</div>
		{:else}
			<div class="rq-card table-container">
				<table class="rq-table">
					<thead>
						<tr><th>Time</th><th>Status</th><th>Queue</th><th>Alert</th><th>Details</th></tr>
					</thead>
					<tbody>
						{#each overview.history as h, i (i)}
							<tr>
								<td class="text-surface-400 whitespace-nowrap" title={new Date(h.time).toLocaleString()}>{formatRelativeTime(h.time)}</td>
								<td><span class={statusBadges[h.status] ?? 'badge preset-outlined-surface-500 text-xs'}>{h.status}</span></td>
								<td>{h.queue}</td>
								<td>{ruleLabels[h.rule] ?? h.rule}</td>
								<td class="text-surface-400">{h.message}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>
</div>
