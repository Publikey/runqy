export interface WorkerQueue {
	name: string;
	priority: number;
}

// Parse a worker heartbeat "queues" field into {name, priority} entries, sorted by
// priority (desc) then name. Current workers write JSON ({"q.high":10,"q.low":1});
// older ones wrote Go's "%v" form ("map[q.high:10 q.low:1]").
export function parseWorkerQueues(queuesStr: string | null | undefined): WorkerQueue[] {
	const s = (queuesStr ?? '').trim();
	if (!s) return [];

	let queues: WorkerQueue[] = [];
	if (s.startsWith('{')) {
		try {
			const obj = JSON.parse(s) as Record<string, number>;
			queues = Object.entries(obj).map(([name, priority]) => ({ name, priority: Number(priority) || 0 }));
		} catch {
			return [];
		}
	} else {
		const inner = s.replace(/^map\[/, '').replace(/]$/, '');
		queues = inner
			.split(/[\s,]+/)
			.filter(Boolean)
			.map((tok) => {
				const i = tok.lastIndexOf(':');
				if (i < 0) return { name: tok, priority: 0 };
				return { name: tok.slice(0, i), priority: parseInt(tok.slice(i + 1), 10) || 0 };
			})
			.filter((q) => q.name);
	}

	return queues.sort((a, b) => b.priority - a.priority || a.name.localeCompare(b.name));
}

// Queue names a worker listens on.
export function workerQueueNames(queuesStr: string | null | undefined): string[] {
	return parseWorkerQueues(queuesStr).map((q) => q.name);
}
