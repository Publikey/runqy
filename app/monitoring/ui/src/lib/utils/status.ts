// One place that turns domain states into a badge tone and label, so every page shows a
// given status the same way. Tones map to .rq-badge-* classes (see app.css).
import type { ServerInfo, Worker } from '$lib/api/types';

export type Tone = 'neutral' | 'accent' | 'success' | 'warning' | 'danger';

export interface StatusView {
	label: string;
	tone: Tone;
}

const unknown = (s: string): StatusView => ({ label: s || 'Unknown', tone: 'neutral' });

// ---- Queues ----

export function queueStatus(paused: boolean): StatusView {
	return paused ? { label: 'Paused', tone: 'warning' } : { label: 'Running', tone: 'success' };
}

// ---- Workers ----

export type WorkerState =
	| 'processing'
	| 'idle'
	| 'bootstrapping'
	| 'stale'
	| 'draining'
	| 'drained'
	| 'stopped';

/** A worker is processing when its asynq server reports in-flight tasks. */
export function isWorkerProcessing(workerId: string, servers: ServerInfo[]): boolean {
	return servers.some((s) => s.id === workerId && (s.active_workers?.length ?? 0) > 0);
}

export function workerState(worker: Worker, servers: ServerInfo[]): WorkerState {
	if (worker.is_stale) return 'stale';
	if (worker.status === 'bootstrapping') return 'bootstrapping';
	if (worker.status === 'draining' || worker.status === 'drained') return worker.status;
	if (worker.status === 'running') {
		return isWorkerProcessing(worker.worker_id, servers) ? 'processing' : 'idle';
	}
	return 'stopped';
}

const WORKER: Record<WorkerState, StatusView> = {
	processing: { label: 'Processing', tone: 'accent' },
	idle: { label: 'Idle', tone: 'success' },
	bootstrapping: { label: 'Bootstrapping', tone: 'accent' },
	stale: { label: 'Stale', tone: 'warning' },
	draining: { label: 'Draining', tone: 'warning' },
	drained: { label: 'Drained', tone: 'neutral' },
	stopped: { label: 'Stopped', tone: 'neutral' }
};

export function workerStatus(state: WorkerState): StatusView {
	return WORKER[state];
}

// ---- Tasks ----

const TASK: Record<string, StatusView> = {
	active: { label: 'Active', tone: 'accent' },
	pending: { label: 'Pending', tone: 'neutral' },
	scheduled: { label: 'Scheduled', tone: 'neutral' },
	retry: { label: 'Retry', tone: 'warning' },
	archived: { label: 'Archived', tone: 'danger' },
	completed: { label: 'Completed', tone: 'success' }
};

export function taskStatus(state: string | undefined): StatusView {
	return TASK[state ?? 'pending'] ?? unknown(state ?? '');
}

// ---- Autoscale instances ----

const INSTANCE: Record<string, StatusView> = {
	running: { label: 'Running', tone: 'success' },
	booting: { label: 'Booting', tone: 'accent' },
	provisioning: { label: 'Provisioning', tone: 'accent' },
	draining: { label: 'Draining', tone: 'warning' },
	destroying: { label: 'Destroying', tone: 'warning' },
	failed: { label: 'Failed', tone: 'danger' },
	terminated: { label: 'Terminated', tone: 'neutral' }
};

/** A "running" machine without a worker yet is still booting (image pull, deps). */
export function instanceStatus(status: string, workerId?: string): StatusView {
	const s = status === 'running' && !workerId ? 'booting' : status;
	return INSTANCE[s] ?? unknown(s);
}

// ---- Alerts ----

const ALERT: Record<string, StatusView> = {
	firing: { label: 'Firing', tone: 'danger' },
	repeat: { label: 'Repeat', tone: 'danger' },
	pending: { label: 'Pending', tone: 'warning' },
	resolved: { label: 'Resolved', tone: 'success' },
	event: { label: 'Event', tone: 'warning' }
};

export function alertStatus(status: string): StatusView {
	return ALERT[status] ?? unknown(status);
}

// ---- Counts in tables and lists ----

/**
 * Text class for a count: zero is dimmed, an anomaly gets its colour, everything else is
 * plain. Colour marks problems, not normal states (pending work is normal).
 */
export function countClass(value: number, anomaly?: 'warning' | 'danger'): string {
	if (!value) return 'text-fg-faint';
	if (anomaly === 'danger') return 'text-error-300';
	if (anomaly === 'warning') return 'text-warning-300';
	return 'text-fg';
}

// ---- Resource usage (progress bars) ----

export function usageTone(percent: number): Tone {
	if (percent >= 90) return 'danger';
	if (percent >= 75) return 'warning';
	return 'accent';
}
