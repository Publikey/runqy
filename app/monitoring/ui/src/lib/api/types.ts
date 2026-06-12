// Queue types - matches actual API response
export interface Queue {
	queue: string;
	paused: boolean;
	size?: number;
	active: number;
	pending: number;
	scheduled: number;
	retry: number;
	archived: number;
	completed: number;
	processed?: number;
	failed?: number;
	latency_msec?: number;
	memory_usage: number;
}

export interface QueueInfo {
	queue: string;
	paused: boolean;
	size: number;
	groups: number;
	latency_msec: number;
	display_latency: string;
	memory_usage_bytes: number;
	active: number;
	pending: number;
	scheduled: number;
	retry: number;
	archived: number;
	completed: number;
}

// Task types
export interface Task {
	id: string;
	type: string;
	payload: string;
	queue: string;
	state: TaskState;
	max_retry: number;
	retried: number;
	error_message: string;
	last_failed_at: string;
	timeout_seconds: number;
	deadline: string;
	group: string;
	next_process_at: string;
	is_orphaned: boolean;
	retention_minutes: number;
	completed_at: string;
	result: string;
}

export type TaskState = 'active' | 'pending' | 'scheduled' | 'retry' | 'archived' | 'completed' | 'aggregating';

export interface TaskListResponse {
	tasks: Task[];
	stats: {
		total: number;
	};
}

// Worker types - matches actual API response
export interface Worker {
	worker_id: string;
	started_at: number;  // Unix timestamp
	last_beat: number;   // Unix timestamp
	concurrency: number;
	queues: string;      // String like "map[queue1:5 queue2:5]"
	status: string;
	is_stale: boolean;
	metrics?: WorkerMetrics;
}

export interface WorkerMetrics {
	cpu_percent: number;
	memory_used_bytes: number;
	memory_total_bytes: number;
	gpus?: GPUMetrics[];
	collected_at: number;
}

export interface GPUMetrics {
	index: number;
	name: string;
	utilization_percent: number;
	memory_used_mb: number;
	memory_total_mb: number;
	temperature_c: number;
}

export interface LogLine {
	ts: number;
	src: string; // "stderr" or "stdout"
	text: string;
	seq: number;
}

export interface ActiveWorker {
	task_id: string;
	task_type: string;
	task_payload: string;
	queue: string;
	started: string;
	deadline: string;
}

// Server types - matches actual API response
export interface ServerInfo {
	id: string;
	host: string;
	pid: number;
	concurrency: number;
	queues: Record<string, number>;
	strict_priority: boolean;
	status: string;
	started: string;
	active_workers: ActiveWorker[];
}

// Redis types
export interface RedisInfo {
	address: string;
	info: RedisInfoDetails;
	raw_info: string;
	cluster: RedisClusterInfo | null;
}

export interface RedisInfoDetails {
	version: string;
	uptime_in_days: string;
	connected_clients: string;
	used_memory_human: string;
	used_memory_peak_human: string;
	cluster_enabled: string;
}

export interface RedisClusterInfo {
	cluster_state: string;
	cluster_slots_assigned: string;
	cluster_slots_ok: string;
	cluster_known_nodes: string;
	cluster_size: string;
}

// Stats types
export interface DailyStats {
	date: string;
	processed: number;
	failed: number;
}

// Database types
export interface DatabaseInfo {
	type: string;
	connected: boolean;
	host?: string;
	database?: string;
	stats?: DatabaseStats;
}

export interface DatabaseStats {
	open_connections: number;
	in_use: number;
	idle: number;
}

export interface EnvVar {
	name: string;
	value: string;
}

export interface SystemEnvInfo {
	vars: EnvVar[];
}

// Metrics types (Prometheus data)
export interface PrometheusResult {
	status: string;
	data: {
		resultType: string;
		result: PrometheusMetric[];
	};
}

export interface PrometheusMetric {
	metric: Record<string, string>;
	values: [number, string][]; // [timestamp, value]
}

export interface MetricsResponse {
	queue_size?: PrometheusResult;
	queue_latency_seconds?: PrometheusResult;
	queue_memory_usage_approx_bytes?: PrometheusResult;
	tasks_processed_per_second?: PrometheusResult;
	tasks_failed_per_second?: PrometheusResult;
	error_rate?: PrometheusResult;
	pending_tasks_by_queue?: PrometheusResult;
	retry_tasks_by_queue?: PrometheusResult;
	archived_tasks_by_queue?: PrometheusResult;
}

export interface QueueStatsResponse {
	stats: { [queueName: string]: DailyStats[] };
}

// API response wrappers
export interface ApiResponse<T> {
	data?: T;
	error?: string;
}

export interface PagedResponse<T> {
	items: T[];
	page_size: number;
	page: number;
}

// Vault types
export interface VaultSummary {
	name: string;
	description: string;
	entry_count: number;
}

export interface VaultEntryView {
	key: string;
	value: string; // Masked if is_secret is true
	is_secret: boolean;
	created_at: string;
	updated_at: string;
}

export interface VaultDetail {
	name: string;
	description: string;
	entries: VaultEntryView[];
	created_at: string;
	updated_at: string;
}

// Queue config types
export interface QueueConfigSummary {
	name: string;
	priority: number;
}

export interface DeploymentConfig {
	git_url: string;
	branch: string;
	code_path?: string;
	startup_cmd: string;
	mode?: string;
	startup_timeout_secs: number;
	redis_storage?: boolean;
	vaults?: string[];
	git_token?: string;
}

export interface QueueConfigDetail {
	name: string;
	priority: number;
	deployment?: DeploymentConfig;
	created_at: number;
	updated_at: number;
}
