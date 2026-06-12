import { writable } from 'svelte/store';
import { browser } from '$app/environment';

interface Settings {
	pollInterval: number;
	sidebarCollapsed: boolean;
}

const DEFAULT_SETTINGS: Settings = {
	pollInterval: 5,
	sidebarCollapsed: false
};

function loadSettings(): Settings {
	if (!browser) return DEFAULT_SETTINGS;

	try {
		const stored = localStorage.getItem('runqy-settings');
		if (stored) {
			// Drop removed legacy keys (theme, viewDensity).
			const { theme: _theme, viewDensity: _viewDensity, ...parsed } = JSON.parse(stored);
			return { ...DEFAULT_SETTINGS, ...parsed };
		}
	} catch {
		// Ignore parse errors
	}
	return DEFAULT_SETTINGS;
}

function createSettingsStore() {
	const { subscribe, set, update } = writable<Settings>(loadSettings());

	if (browser) {
		subscribe((value) => {
			localStorage.setItem('runqy-settings', JSON.stringify(value));
		});
	}

	return {
		subscribe,
		setPollInterval: (pollInterval: number) => update((s) => ({ ...s, pollInterval })),
		toggleSidebar: () => update((s) => ({ ...s, sidebarCollapsed: !s.sidebarCollapsed })),
		reset: () => set(DEFAULT_SETTINGS)
	};
}

export const settings = createSettingsStore();
