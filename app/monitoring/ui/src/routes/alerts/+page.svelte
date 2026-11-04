<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { base } from '$app/paths';
	import { settings as uiSettings } from '$lib/stores/settings';
	import { toaster } from '$lib/stores/toaster';
	import { getAlertsOverview, getAlertSettings, saveAlertSettings, sendTestAlert } from '$lib/api/client';
	import type { AlertsOverview, AlertSettings, AlertRule } from '$lib/api/types';
	import { formatRelativeTime } from '$lib/utils/format';
	import AlertRulesEditor from '$lib/components/AlertRulesEditor.svelte';
	import { Bell, CircleCheck, Pencil, Send } from '@lucide/svelte';
	import { Callout, Badge, Button, EmptyState, Field, PageHeader, Panel, Segmented, Shimmer, Toggle } from '$lib/components/ui';
	import { alertStatus } from '$lib/utils/status';

	const ruleLabels: Record<string, string> = {
		no_workers: 'No workers',
		latency: 'Latency',
		pending: 'Backlog',
		worker_unhealthy: 'Unhealthy worker',
		autoscale_failure: 'Autoscaling failure'
	};

	let overview = $state<AlertsOverview | null>(null);
	let loadError = $state('');
	let pollInterval: ReturnType<typeof setInterval> | null = null;

	// Settings form
	let form = $state<AlertSettings>(emptySettings());
	let recipientsText = $state('');
	let rules = $state<AlertRule[]>([]);
	let sendResolved = $state(true);
	let saving = $state(false);
	let testing = $state(false);
	let settingsLoaded = $state(false);
	// Saved state (not the form being edited): decides whether the card starts folded.
	let saved = $state<AlertSettings | null>(null);
	let editing = $state(false);
	let listTab = $state<'active' | 'history'>('active');

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
			saved = settings;
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
			editing = false;
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

	onMount(() => {
		loadOverview();
		loadSettings();
		pollInterval = setInterval(loadOverview, $uiSettings.pollInterval * 1000);
	});

	onDestroy(() => {
		if (pollInterval) clearInterval(pollInterval);
	});

	let firingCount = $derived(overview?.active.filter((a) => a.firing).length ?? 0);

	// Configured = enabled with somewhere to send: then the settings fold into a summary.
	let configured = $derived(Boolean(saved?.enabled && (saved.recipients?.length ?? 0) > 0));
	let showForm = $derived(settingsLoaded && (!configured || editing));

	// The last send failed if its error is newer than the last successful send.
	let deliveryError = $derived.by(() => {
		const st = overview?.status;
		if (!st?.last_error) return '';
		if (st.last_sent && st.last_error_at && new Date(st.last_sent) > new Date(st.last_error_at)) return '';
		return st.last_error;
	});

	function cancelEdit() {
		editing = false;
		loadSettings(); // drop unsaved changes
	}

	// Delivery state and last evaluation live in the notifications card; the header only says
	// whether something is going on.
	let subtitle = $derived.by(() => {
		if (!overview) return 'Loading...';
		if (firingCount > 0) return `${firingCount} alert${firingCount !== 1 ? 's' : ''} firing`;
		return overview.active.length > 0 ? `${overview.active.length} pending, nothing firing` : 'Nothing firing';
	});
</script>

<svelte:head>
	<title>Alerts - runqy Monitor</title>
</svelte:head>

<div class="rq-page space-y-4 md:space-y-6">
	<PageHeader title="Alerts" {subtitle} onrefresh={loadOverview} />

	{#if loadError}
		<Callout>{loadError}</Callout>
	{/if}

	<!-- How alerts are delivered: a one-line summary once configured, the form otherwise. -->
	<Panel title="Email notifications" footer={showForm ? settingsFooter : undefined}>
		{#snippet titleExtra()}
			{#if settingsLoaded}
				{#if deliveryError}
					<Badge tone="danger" dot>Delivery failing</Badge>
				{:else if saved?.enabled}
					<Badge tone="success" dot>Enabled</Badge>
				{:else}
					<Badge tone="outline">Disabled</Badge>
				{/if}
			{/if}
		{/snippet}
		{#snippet actions()}
			{#if settingsLoaded && configured && !editing}
				<Button variant="secondary" size="sm" icon={Send} onclick={handleTest} loading={testing}>Send test</Button>
				<Button variant="ghost" size="sm" icon={Pencil} onclick={() => (editing = true)}>Edit</Button>
			{/if}
		{/snippet}

		{#if !settingsLoaded}
			<Shimmer lines={2} />
		{:else}
			{#if deliveryError}
				<Callout class="mb-4" title="Last delivery failed">
					<p class="mt-0.5 break-words">{deliveryError}</p>
					{#if overview?.status.last_error_at}
						<p class="mt-1 text-xs text-error-300">{formatRelativeTime(overview.status.last_error_at)}</p>
					{/if}
				</Callout>
			{/if}

			{#if configured && saved}
				{@const recipients = saved.recipients ?? []}
				{@const ruleCount = saved.rules?.length ?? 0}
				<p class="flex flex-wrap items-center gap-x-2 gap-y-1 text-ui text-fg-muted {showForm ? 'mb-5' : ''}">
					<span class="text-fg">{saved.provider === 'sendgrid' ? 'SendGrid' : 'SMTP'}</span>
					<span class="text-fg-faint">→</span>
					<span title={recipients.join(', ')}>{recipients.length} recipient{recipients.length !== 1 ? 's' : ''}</span>
					<span class="text-fg-faint">·</span>
					<span>{ruleCount} default rule{ruleCount !== 1 ? 's' : ''}</span>
					<span class="text-fg-faint">·</span>
					<span>
						{#if overview?.status.last_sent}
							last sent {formatRelativeTime(overview.status.last_sent)}
						{:else}
							nothing sent yet
						{/if}
					</span>
					{#if overview?.status.last_eval}
						<span class="text-fg-faint">·</span>
						<span class="text-fg-subtle">evaluated {formatRelativeTime(overview.status.last_eval)}</span>
					{/if}
				</p>
			{/if}

			{#if showForm}
				{#if !configured}
					<p class="mb-5 text-ui text-fg-muted">
						Turn on email notifications and add at least one recipient to be told when a queue has no worker,
						falls behind, or a worker turns unhealthy.
					</p>
				{/if}
				<div class="space-y-6">
				<Toggle
					bind:checked={form.enabled}
					label="Email notifications"
					description="A top-level alerts block in a queue YAML file replaces these settings on the next config reload."
					disabled={saving}
				/>

				<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
					<Field label="Provider" for="alerts-provider">
						<select id="alerts-provider" bind:value={form.provider} class="select" disabled={saving}>
							<option value="smtp">SMTP</option>
							<option value="sendgrid">SendGrid</option>
						</select>
					</Field>
					<Field label="From" for="alerts-from">
						<input
							id="alerts-from"
							type="text"
							bind:value={form.from}
							placeholder="runqy <alerts@example.com>"
							class="input"
							disabled={saving}
						/>
					</Field>
					<Field label="Recipients" for="alerts-recipients" hint="One per line." class="md:col-span-2">
						<textarea
							id="alerts-recipients"
							bind:value={recipientsText}
							rows="3"
							placeholder="ops@example.com"
							class="textarea"
							disabled={saving}
						></textarea>
					</Field>
				</div>

				{#if form.provider === 'smtp' && form.smtp}
					<div class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-4 gap-4">
						<Field label="SMTP host" for="alerts-smtp-host" class="sm:col-span-2">
							<input
								id="alerts-smtp-host"
								type="text"
								bind:value={form.smtp.host}
								placeholder="smtp.example.com"
								class="input"
								disabled={saving}
							/>
						</Field>
						<Field label="Security" for="alerts-smtp-tls">
							<select id="alerts-smtp-tls" bind:value={form.smtp.tls} class="select" disabled={saving}>
								<option value="starttls">STARTTLS (587)</option>
								<option value="tls">TLS (465)</option>
								<option value="none">None (25)</option>
							</select>
						</Field>
						<Field label="Port (optional)" for="alerts-smtp-port">
							<input
								id="alerts-smtp-port"
								type="number"
								bind:value={form.smtp.port}
								min="1"
								max="65535"
								class="input"
								disabled={saving}
							/>
						</Field>
						<Field label="Username" for="alerts-smtp-user" class="sm:col-span-2">
							<input
								id="alerts-smtp-user"
								type="text"
								bind:value={form.smtp.username}
								autocomplete="off"
								class="input"
								disabled={saving}
							/>
						</Field>
						<Field label="Password" for="alerts-smtp-password" class="sm:col-span-2">
							<input
								id="alerts-smtp-password"
								type="password"
								bind:value={form.smtp.password}
								autocomplete="new-password"
								placeholder="vault://vault/key, or empty to use RUNQY_SMTP_PASSWORD"
								class="input"
								disabled={saving}
							/>
						</Field>
					</div>
				{:else if form.provider === 'sendgrid' && form.sendgrid}
					<Field label="SendGrid API key" for="alerts-sendgrid-key">
						<input
							id="alerts-sendgrid-key"
							type="password"
							bind:value={form.sendgrid.api_key}
							autocomplete="new-password"
							placeholder="vault://vault/key, or empty to use RUNQY_SENDGRID_API_KEY"
							class="input"
							disabled={saving}
						/>
					</Field>
				{/if}

				<div class="grid grid-cols-1 sm:grid-cols-2 gap-4 sm:items-end">
					<Field label="Repeat while firing" for="alerts-repeat">
						<input
							id="alerts-repeat"
							type="text"
							bind:value={form.repeat_interval}
							placeholder="4h (0 = once)"
							class="input"
							disabled={saving}
						/>
					</Field>
					<div class="sm:pb-1.5">
						<Toggle bind:checked={sendResolved} label="Notify when an alert resolves" disabled={saving} />
					</div>
				</div>

				<div class="space-y-3 border-t border-line-subtle pt-5">
					<div>
						<h3 class="text-sm font-semibold text-fg">Default rules</h3>
						<p class="mt-0.5 text-ui text-fg-subtle">
							Applied to every queue. A queue can override or turn off a rule in its configuration.
						</p>
					</div>
					<AlertRulesEditor bind:rules disabled={saving} />
				</div>
				</div>
			{/if}
		{/if}

	</Panel>

	<!-- What is happening: alerts in progress, then what was sent -->
	<Panel title="Alerts" flush>
		{#snippet actions()}
			<Segmented
				name="alertsList"
				label="Alerts list"
				items={[
					{ value: 'active', label: 'Active', count: overview?.active.length ?? 0 },
					{ value: 'history', label: 'History', count: overview?.history.length ?? 0 }
				]}
				bind:value={listTab}
			/>
		{/snippet}

		{#if listTab === 'active'}
			{#if !overview || overview.active.length === 0}
				<EmptyState icon={CircleCheck} title="No active alerts" description="Every queue is within its rules." compact />
			{:else}
				<div class="table-container">
					<table class="rq-table">
						<thead>
							<tr><th>State</th><th>Queue</th><th>Alert</th><th>Since</th><th>Details</th></tr>
						</thead>
						<tbody>
							{#each overview.active as a (a.queue + a.rule)}
								<tr>
									<td><Badge status={alertStatus(a.firing ? 'firing' : 'pending')} /></td>
									<td class="font-mono">
										<a class="text-primary-400 hover:text-primary-300" href="{base}/queues/{encodeURIComponent(a.queue)}"
											>{a.queue}</a
										>
									</td>
									<td>{ruleLabels[a.rule] ?? a.rule}</td>
									<td class="text-fg-subtle whitespace-nowrap">
										{formatRelativeTime(a.firing && a.fired_at ? a.fired_at : a.since)}
									</td>
									<td class="text-fg-muted">{a.message}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		{:else if !overview || overview.history.length === 0}
			<EmptyState icon={Bell} title="No notifications sent yet" compact />
		{:else}
			<div class="table-container">
				<table class="rq-table">
					<thead>
						<tr><th>Time</th><th>Status</th><th>Queue</th><th>Alert</th><th>Details</th></tr>
					</thead>
					<tbody>
						{#each overview.history as h, i (i)}
							<tr>
								<td class="text-fg-subtle whitespace-nowrap" title={new Date(h.time).toLocaleString()}>
									{formatRelativeTime(h.time)}
								</td>
								<td><Badge status={alertStatus(h.status)} /></td>
								<td class="font-mono">{h.queue}</td>
								<td>{ruleLabels[h.rule] ?? h.rule}</td>
								<td class="text-fg-muted">{h.message}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</Panel>
</div>

{#snippet settingsFooter()}
	<div class="flex flex-wrap justify-end gap-2">
		{#if configured}
			<Button variant="ghost" onclick={cancelEdit} disabled={saving}>Cancel</Button>
		{/if}
		<Button variant="secondary" icon={Send} onclick={handleTest} loading={testing} disabled={saving}>
			{testing ? 'Sending...' : 'Send test email'}
		</Button>
		<Button variant="primary" onclick={handleSave} loading={saving}>
			{saving ? 'Saving...' : 'Save settings'}
		</Button>
	</div>
{/snippet}
