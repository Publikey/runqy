import type { Queue } from '$lib/api/types';

export interface QueueGroup {
	name: string;
	queues: Queue[];
	// Aggregated stats
	pending: number;
	active: number;
	completed: number;
	retry: number;
	archived: number;
	failed: number;
	paused: boolean; // true if all sub-queues are paused
	memory_usage: number;
	latency_msec: number; // max latency
}

/**
 * Parse a queue name into parent and sub-queue parts.
 * Format: "parent.subqueue" -> { parent: "parent", subqueue: "subqueue" }
 * If no dot, returns { parent: queueName, subqueue: null }
 */
export function parseQueueName(queueName: string): { parent: string; subqueue: string | null } {
	const lastDotIndex = queueName.lastIndexOf('.');
	if (lastDotIndex === -1) {
		return { parent: queueName, subqueue: null };
	}
	return {
		parent: queueName.substring(0, lastDotIndex),
		subqueue: queueName.substring(lastDotIndex + 1)
	};
}

/**
 * Group queues by their parent name.
 * Returns an array of QueueGroup objects with aggregated stats.
 */
export function groupQueues(queues: Queue[]): QueueGroup[] {
	const groupMap = new Map<string, Queue[]>();

	// Group queues by parent name
	for (const queue of queues) {
		const { parent } = parseQueueName(queue.queue);
		if (!groupMap.has(parent)) {
			groupMap.set(parent, []);
		}
		groupMap.get(parent)!.push(queue);
	}

	// Convert to QueueGroup array with aggregated stats
	const groups: QueueGroup[] = [];
	for (const [name, queueList] of groupMap) {
		// Sort sub-queues by name for consistent ordering
		queueList.sort((a, b) => a.queue.localeCompare(b.queue));

		const group: QueueGroup = {
			name,
			queues: queueList,
			pending: queueList.reduce((sum, q) => sum + (q.pending || 0), 0),
			active: queueList.reduce((sum, q) => sum + (q.active || 0), 0),
			completed: queueList.reduce((sum, q) => sum + (q.completed || 0), 0),
			retry: queueList.reduce((sum, q) => sum + (q.retry || 0), 0),
			archived: queueList.reduce((sum, q) => sum + (q.archived || 0), 0),
			failed: queueList.reduce((sum, q) => sum + (q.failed || 0), 0),
			paused: queueList.every((q) => q.paused),
			memory_usage: queueList.reduce((sum, q) => sum + (q.memory_usage || 0), 0),
			latency_msec: Math.max(...queueList.map((q) => q.latency_msec || 0))
		};
		groups.push(group);
	}

	// Sort groups by name
	groups.sort((a, b) => a.name.localeCompare(b.name));

	return groups;
}
