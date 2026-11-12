// Human-readable views of a queue's autoscale config, shared by the Autoscaling page
// and the queue detail page.
import type { AutoscaleConfig, AutoscaleInstance, ScaleTrigger } from '$lib/api/types';

/** Instances that provide (or are about to provide) capacity, or are being torn down. */
export const ACTIVE_INSTANCE_STATUSES = ['provisioning', 'running', 'draining', 'destroying'];

export function isActiveInstance(i: AutoscaleInstance): boolean {
	return ACTIVE_INSTANCE_STATUSES.includes(i.status);
}

function subQueuesSuffix(t: ScaleTrigger): string {
	return t.sub_queues?.length ? ` [${t.sub_queues.join(', ')}]` : '';
}

export function describeTrigger(t: ScaleTrigger, direction: 'up' | 'down'): string {
	switch (t.trigger) {
		case 'no_workers':
			return 'no workers' + subQueuesSuffix(t);
		case 'queue_depth':
			return `pending ≥ ${t.threshold ?? '?'}` + subQueuesSuffix(t);
		case 'latency':
			return `latency > ${t.max_latency ?? '?'}` + subQueuesSuffix(t);
		case 'idle':
			return `idle ${t.timeout ?? '?'}`;
		case 'schedule':
			if (direction === 'up') {
				return `cron "${t.cron}" → ${t.workers ?? '?'}${t.duration ? ` for ${t.duration}` : ''}`;
			}
			return `cron "${t.cron}" → min`;
		default:
			return t.trigger;
	}
}

/** Instance spec as short parts: GPU first, then disk, RAM, vCPU, price cap, image. */
export function describeInstance(asc: AutoscaleConfig): string[] {
	const out: string[] = [];
	const spec = asc.instance || {};
	if (spec.gpu) out.push(spec.gpu);
	if (spec.disk_gb) out.push(`${spec.disk_gb} GB disk`);
	if (spec.min_ram_gb) out.push(`≥ ${spec.min_ram_gb} GB RAM`);
	if (spec.min_vcpu) out.push(`≥ ${spec.min_vcpu} vCPU`);
	if (spec.min_download_mbps) out.push(`≥ ${spec.min_download_mbps} Mbps down`);
	if (spec.max_price_per_hour) out.push(`≤ $${spec.max_price_per_hour}/hr`);
	if (spec.image) out.push(spec.image);
	return out;
}

/** "12:59 (in 4 min)" for a provisioning pause end. */
export function formatUntil(until: string): string {
	const d = new Date(until);
	const mins = Math.max(0, Math.round((d.getTime() - Date.now()) / 60000));
	const time = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
	if (mins >= 120) return `${time} (in ${Math.round(mins / 60)} h)`;
	return mins >= 1 ? `${time} (in ${mins} min)` : `${time} (< 1 min)`;
}
