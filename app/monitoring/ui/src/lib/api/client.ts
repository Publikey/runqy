import { base } from '$app/paths';
import { browser } from '$app/environment';
import type {
	Queue,
	QueueInfo,
	Task,
	TaskListResponse,
	TaskState,
	Worker,
	WorkerInfo,
	ServerInfo,
	RedisInfo,
	DailyStats,
	DatabaseInfo,
	SystemEnvInfo,
	VaultSummary,
	VaultDetail,
	MetricsResponse,
	QueueStatsResponse,
	QueueConfigDetail,
	DeploymentConfig,
	PlaygroundEnqueueResponse
} from './types';

const BASE_URL = `${base}/api`;

// Custom error class for auth errors
export class AuthError extends Error {
	constructor(message: string) {
		super(message);
		this.name = 'AuthError';
	}
}

async function fetchJson<T>(url: string, options?: RequestInit): Promise<T> {
	const response = await fetch(url, {
		...options,
		credentials: 'include',
		headers: {
			'Content-Type': 'application/json',
			...options?.headers
		}
	});

	// Handle 401 Unauthorized - redirect to login
	if (response.status === 401) {
		if (browser) {
			// Trigger a re-check of auth status which will redirect to login
			window.dispatchEvent(new CustomEvent('auth:unauthorized'));
		}
		throw new AuthError('Unauthorized');
	}

	if (!response.ok) {
		const errorText = await response.text();
		throw new Error(errorText || `HTTP ${response.status}`);
	}

	// Handle empty responses (for void returns like pause/resume/delete)
	const text = await response.text();
	if (!text) {
		return {} as T;
	}

	try {
		return JSON.parse(text);
	} catch {
		return {} as T;
	}
}

// Queue API
export async function getQueues(): Promise<{ queues: Queue[] }> {
	return fetchJson(`${BASE_URL}/queues`);
}

export async function getQueueInfo(qname: string): Promise<QueueInfo> {
	const data = await fetchJson<{ current: QueueInfo }>(`${BASE_URL}/queues/${encodeURIComponent(qname)}`);
	return data.current;
}

export async function pauseQueue(qname: string): Promise<void> {
	await fetchJson(`${BASE_URL}/queues/${encodeURIComponent(qname)}:pause`, {
		method: 'POST'
	});
}

export async function resumeQueue(qname: string): Promise<void> {
	await fetchJson(`${BASE_URL}/queues/${encodeURIComponent(qname)}:resume`, {
		method: 'POST'
	});
}

export async function deleteQueue(qname: string, force: boolean = true): Promise<void> {
	const params = force ? '?force=true' : '';
	await fetchJson(`${BASE_URL}/queues/${encodeURIComponent(qname)}${params}`, {
		method: 'DELETE'
	});
}

export async function restoreQueue(qname: string): Promise<void> {
	await fetchJson(`${BASE_URL}/queues/${encodeURIComponent(qname)}:restore`, {
		method: 'POST'
	});
}

// Task API
export async function getTasks(
	qname: string,
	state: TaskState,
	page: number = 1,
	pageSize: number = 20
): Promise<TaskListResponse> {
	// NB: the backend reads `size` (getPageOptions), not `page_size`.
	return fetchJson(
		`${BASE_URL}/queues/${encodeURIComponent(qname)}/${state}_tasks?size=${pageSize}&page=${page}`
	);
}

// Fetch a single task directly (any state)
export async function getTask(qname: string, taskId: string): Promise<Task> {
	return fetchJson(
		`${BASE_URL}/queues/${encodeURIComponent(qname)}/tasks/${encodeURIComponent(taskId)}`
	);
}

export async function getTaskById(qname: string, taskId: string): Promise<Task> {
	// Try each state until we find the task
	const states: TaskState[] = ['active', 'pending', 'scheduled', 'retry', 'archived', 'completed'];
	for (const state of states) {
		try {
			const response = await getTasks(qname, state, 1, 100);
			const task = response.tasks?.find((t) => t.id === taskId);
			if (task) return task;
		} catch {
			// Continue to next state
		}
	}
	throw new Error('Task not found');
}

export async function cancelTask(qname: string, taskId: string): Promise<void> {
	await fetchJson(
		`${BASE_URL}/queues/${encodeURIComponent(qname)}/active_tasks/${encodeURIComponent(taskId)}:cancel`,
		{ method: 'POST' }
	);
}

export async function deleteTask(qname: string, taskId: string, state: TaskState): Promise<void> {
	await fetchJson(
		`${BASE_URL}/queues/${encodeURIComponent(qname)}/${state}_tasks/${encodeURIComponent(taskId)}`,
		{ method: 'DELETE' }
	);
}

export async function runTask(qname: string, taskId: string): Promise<void> {
	await fetchJson(
		`${BASE_URL}/queues/${encodeURIComponent(qname)}/scheduled_tasks/${encodeURIComponent(taskId)}:run`,
		{ method: 'POST' }
	);
}

export async function archiveTask(qname: string, taskId: string, state: TaskState): Promise<void> {
	await fetchJson(
		`${BASE_URL}/queues/${encodeURIComponent(qname)}/${state}_tasks/${encodeURIComponent(taskId)}:archive`,
		{ method: 'POST' }
	);
}

export async function runAllScheduledTasks(qname: string): Promise<{ run_count: number }> {
	return fetchJson(
		`${BASE_URL}/queues/${encodeURIComponent(qname)}/scheduled_tasks:run_all`,
		{ method: 'POST' }
	);
}

export async function archiveAllTasks(qname: string, state: TaskState): Promise<{ archived_count: number }> {
	return fetchJson(
		`${BASE_URL}/queues/${encodeURIComponent(qname)}/${state}_tasks:archive_all`,
		{ method: 'POST' }
	);
}

export async function deleteAllTasks(qname: string, state: TaskState): Promise<{ deleted_count: number }> {
	return fetchJson(
		`${BASE_URL}/queues/${encodeURIComponent(qname)}/${state}_tasks:delete_all`,
		{ method: 'POST' }
	);
}

export async function runAllRetryTasks(qname: string): Promise<{ run_count: number }> {
	return fetchJson(
		`${BASE_URL}/queues/${encodeURIComponent(qname)}/retry_tasks:run_all`,
		{ method: 'POST' }
	);
}

export async function runAllArchivedTasks(qname: string): Promise<{ run_count: number }> {
	return fetchJson(
		`${BASE_URL}/queues/${encodeURIComponent(qname)}/archived_tasks:run_all`,
		{ method: 'POST' }
	);
}

// Worker API
export async function getWorkers(): Promise<{ workers: Worker[] }> {
	return fetchJson(`${BASE_URL}/workers`);
}

export async function getWorkerInfo(workerId: string): Promise<WorkerInfo> {
	return fetchJson(`${BASE_URL}/workers/${encodeURIComponent(workerId)}`);
}

export async function getWorkerLogs(workerId: string, n: number = 200): Promise<{ lines: string[]; count: number }> {
	return fetchJson(`${BASE_URL}/workers/${encodeURIComponent(workerId)}/logs?n=${n}`);
}

// Server API
export async function getServers(): Promise<{ servers: ServerInfo[] }> {
	return fetchJson(`${BASE_URL}/servers`);
}

// Redis API
export async function getRedisInfo(): Promise<RedisInfo> {
	return fetchJson(`${BASE_URL}/redis_info`);
}

// Database API
export async function getDatabaseInfo(): Promise<DatabaseInfo> {
	return fetchJson(`${BASE_URL}/database_info`);
}

// System environment variables (read-only, secrets masked)
export async function getSystemEnv(): Promise<SystemEnvInfo> {
	return fetchJson(`${BASE_URL}/system/env`);
}

// Stats API
export async function getDailyStats(qname: string, days: number = 7): Promise<{ stats: DailyStats[] }> {
	return fetchJson(`${BASE_URL}/queue_stats?qname=${encodeURIComponent(qname)}&days=${days}`);
}

export async function getQueueStats(): Promise<QueueStatsResponse> {
	// Backend returns 90 days of stats for all queues (hardcoded)
	return fetchJson(`${BASE_URL}/queue_stats`);
}

// Metrics API (Prometheus data)
export interface MetricsOptions {
	duration?: number; // duration in seconds
	endTime?: number; // Unix timestamp
	queues?: string[]; // filter by queue names
}

export async function getMetrics(options: MetricsOptions = {}): Promise<MetricsResponse> {
	const params = new URLSearchParams();
	if (options.duration) {
		params.set('duration', options.duration.toString());
	}
	if (options.endTime) {
		params.set('endtime', options.endTime.toString());
	}
	if (options.queues && options.queues.length > 0) {
		params.set('queues', options.queues.join(','));
	}
	const queryString = params.toString();
	const url = queryString ? `${BASE_URL}/metrics?${queryString}` : `${BASE_URL}/metrics`;
	return fetchJson(url);
}

// Batch operations
export async function batchDeleteTasks(
	qname: string,
	taskIds: string[],
	state: TaskState
): Promise<{ deleted_count: number; failed: string[] }> {
	return fetchJson(
		`${BASE_URL}/queues/${encodeURIComponent(qname)}/${state}_tasks:batch_delete`,
		{
			method: 'POST',
			body: JSON.stringify({ task_ids: taskIds })
		}
	);
}

export async function batchArchiveTasks(
	qname: string,
	taskIds: string[],
	state: TaskState
): Promise<{ archived_count: number; failed: string[] }> {
	return fetchJson(
		`${BASE_URL}/queues/${encodeURIComponent(qname)}/${state}_tasks:batch_archive`,
		{
			method: 'POST',
			body: JSON.stringify({ task_ids: taskIds })
		}
	);
}

export async function batchRunTasks(
	qname: string,
	taskIds: string[]
): Promise<{ run_count: number; failed: string[] }> {
	return fetchJson(
		`${BASE_URL}/queues/${encodeURIComponent(qname)}/scheduled_tasks:batch_run`,
		{
			method: 'POST',
			body: JSON.stringify({ task_ids: taskIds })
		}
	);
}

// Vault API
export async function getVaults(): Promise<{ vaults: VaultSummary[]; count: number }> {
	return fetchJson(`${BASE_URL}/vaults`);
}

export async function getVault(name: string): Promise<VaultDetail> {
	return fetchJson(`${BASE_URL}/vaults/${encodeURIComponent(name)}`);
}

export async function createVault(
	name: string,
	description?: string
): Promise<{ message: string; vault: { name: string; description: string } }> {
	return fetchJson(`${BASE_URL}/vaults`, {
		method: 'POST',
		body: JSON.stringify({ name, description: description || '' })
	});
}

export async function deleteVault(name: string): Promise<{ message: string }> {
	return fetchJson(`${BASE_URL}/vaults/${encodeURIComponent(name)}`, {
		method: 'DELETE'
	});
}

export async function setVaultEntry(
	vaultName: string,
	key: string,
	value: string,
	isSecret: boolean = true
): Promise<{ message: string }> {
	return fetchJson(`${BASE_URL}/vaults/${encodeURIComponent(vaultName)}/entries`, {
		method: 'POST',
		body: JSON.stringify({ key, value, is_secret: isSecret })
	});
}

export async function deleteVaultEntry(
	vaultName: string,
	key: string
): Promise<{ message: string }> {
	return fetchJson(
		`${BASE_URL}/vaults/${encodeURIComponent(vaultName)}/entries/${encodeURIComponent(key)}`,
		{ method: 'DELETE' }
	);
}

// Queue Config API
export async function getQueueConfigs(): Promise<{ queues: QueueConfigDetail[]; count: number }> {
	return fetchJson(`${BASE_URL}/queue_configs`);
}

export async function getQueueConfig(name: string): Promise<QueueConfigDetail> {
	return fetchJson(`${BASE_URL}/queue_configs/${encodeURIComponent(name)}`);
}

export async function createQueueConfig(
	name: string,
	priority: number,
	deployment?: DeploymentConfig,
	force?: boolean
): Promise<{ message: string; queue: QueueConfigDetail }> {
	const params = force ? '?force=true' : '';
	return fetchJson(`${BASE_URL}/queue_configs${params}`, {
		method: 'POST',
		body: JSON.stringify({ name, priority, deployment })
	});
}

export async function updateQueueConfig(
	name: string,
	priority: number,
	deployment?: DeploymentConfig
): Promise<{ message: string; queue: QueueConfigDetail }> {
	return fetchJson(`${BASE_URL}/queue_configs/${encodeURIComponent(name)}`, {
		method: 'PUT',
		body: JSON.stringify({ priority, deployment })
	});
}

export async function deleteQueueConfig(name: string): Promise<{ message: string }> {
	return fetchJson(`${BASE_URL}/queue_configs/${encodeURIComponent(name)}`, {
		method: 'DELETE'
	});
}

export async function restoreQueueConfig(name: string): Promise<{ message: string }> {
	return fetchJson(`${BASE_URL}/queue_configs/${encodeURIComponent(name)}:restore`, {
		method: 'POST'
	});
}

// Playground API
export async function playgroundEnqueue(
	queue: string,
	payload: unknown,
	count: number = 1,
	timeout: number = 0
): Promise<PlaygroundEnqueueResponse> {
	return fetchJson(`${BASE_URL}/playground/enqueue`, {
		method: 'POST',
		body: JSON.stringify({ queue, payload, count, timeout })
	});
}
