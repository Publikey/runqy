import { redirect } from '@sveltejs/kit';
import { base } from '$app/paths';
import { building } from '$app/environment';
import { getQueues } from '$lib/api/client';
import { parseQueueName } from '$lib/utils/queueGrouping';
import type { PageLoad } from './$types';

// Links built from a parent queue name (autoscaling page, alert emails, typed URLs) land
// here, but this page shows one sub-queue. Send them to the only sub-queue, or to the
// queue list filtered on the parent when there are several.
export const load: PageLoad = async ({ params }) => {
	if (building) return;
	const qname = params.qname;
	let names: string[];
	try {
		names = (await getQueues()).queues?.map((q) => q.queue) ?? [];
	} catch {
		return; // the page reports the API error itself
	}
	if (names.includes(qname)) return;
	const subQueues = names.filter((n) => parseQueueName(n).parent === qname);
	if (subQueues.length === 1) {
		redirect(307, `${base}/queues/${encodeURIComponent(subQueues[0])}`);
	}
	if (subQueues.length > 1) {
		redirect(307, `${base}/queues?q=${encodeURIComponent(qname)}`);
	}
};
