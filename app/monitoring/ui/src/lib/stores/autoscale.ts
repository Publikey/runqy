import { writable, derived } from 'svelte/store';
import type { AutoscaleInstance, AutoscaleProvider } from '$lib/api/types';
import { getAutoscaleStatus, getAutoscaleProviders } from '$lib/api/client';

interface AutoscaleState {
	instances: AutoscaleInstance[];
	providers: AutoscaleProvider[];
	totalCost: number;
	loading: boolean;
	error: string | null;
	featureDisabled: boolean;
	lastUpdated: Date | null;
}

function createAutoscaleStore() {
	const { subscribe, set, update } = writable<AutoscaleState>({
		instances: [],
		providers: [],
		totalCost: 0,
		loading: false,
		error: null,
		featureDisabled: false,
		lastUpdated: null
	});

	return {
		subscribe,
		fetch: async () => {
			update((s) => ({ ...s, loading: true, error: null }));

			// Live status (instances + total cost). This does not depend on the
			// encryption master key, so it should generally succeed.
			let instances: AutoscaleInstance[] = [];
			let totalCost = 0;
			let statusError: string | null = null;
			try {
				const status = await getAutoscaleStatus();
				instances = status.instances || [];
				totalCost = status.total_cost || 0;
			} catch (e) {
				statusError = e instanceof Error ? e.message : 'Failed to fetch autoscale status';
			}

			// Providers require the encryption master key. A 503 with a
			// "disabled"/"RUNQY_VAULT_MASTER_KEY" message means the feature is off.
			let providers: AutoscaleProvider[] = [];
			let featureDisabled = false;
			let providerError: string | null = null;
			try {
				const response = await getAutoscaleProviders();
				providers = response.providers || [];
			} catch (e) {
				const message = e instanceof Error ? e.message : 'Failed to fetch providers';
				if (message.includes('disabled') || message.includes('RUNQY_VAULT_MASTER_KEY')) {
					featureDisabled = true;
				} else {
					providerError = message;
				}
			}

			update((s) => ({
				...s,
				instances,
				providers,
				totalCost,
				featureDisabled,
				loading: false,
				error: statusError || providerError,
				lastUpdated: new Date()
			}));
		},
		clear: () => {
			set({
				instances: [],
				providers: [],
				totalCost: 0,
				loading: false,
				error: null,
				featureDisabled: false,
				lastUpdated: null
			});
		}
	};
}

export const autoscaleStore = createAutoscaleStore();

export const liveInstanceCount = derived(autoscaleStore, ($state) =>
	$state.instances.filter(
		(i) => i.status !== 'terminated' && i.status !== 'destroying' && i.status !== 'failed'
	).length
);
