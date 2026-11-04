// Connection state of the dashboard, fed by every API call (see fetchJson), and the
// "refresh now" requests sent by the global Live indicator.
import { writable } from 'svelte/store';

export interface LiveState {
	/** Last time the server answered (ms since epoch). */
	lastOk: number | null;
	/** The last request could not reach the server. */
	failing: boolean;
}

export const live = writable<LiveState>({ lastOk: null, failing: false });

export function markReachable() {
	live.set({ lastOk: Date.now(), failing: false });
}

export function markUnreachable() {
	live.update((s) => ({ ...s, failing: true }));
}

/** Incremented when the user asks for a refresh; pages listen through PageHeader. */
export const refreshRequests = writable(0);

export function requestRefresh() {
	refreshRequests.update((n) => n + 1);
}
