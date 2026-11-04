<script lang="ts">
	import { goto } from '$app/navigation';
	import { base } from '$app/paths';
	import { authStore } from '$lib/stores/auth';
	import AuthShell from '$lib/components/AuthShell.svelte';
	import { Button, Field } from '$lib/components/ui';

	let email = $state('');
	let password = $state('');

	async function handleSubmit(e: Event) {
		e.preventDefault();
		const success = await authStore.login(email, password);
		if (success) {
			goto(base || '/');
		}
	}
</script>

<svelte:head>
	<title>Sign in · runqy</title>
</svelte:head>

<AuthShell title="Sign in to runqy" subtitle="Monitor your queues, workers and GPU autoscaling." error={$authStore.error}>
	<form onsubmit={handleSubmit} class="space-y-4">
		<Field label="Email" for="email">
			<input
				id="email"
				type="email"
				class="input"
				placeholder="admin@example.com"
				autocomplete="username"
				bind:value={email}
				required
				disabled={$authStore.loading}
			/>
		</Field>

		<Field label="Password" for="password">
			<input
				id="password"
				type="password"
				class="input"
				placeholder="Your password"
				autocomplete="current-password"
				bind:value={password}
				required
				disabled={$authStore.loading}
			/>
		</Field>

		<Button type="submit" variant="primary" class="w-full !h-9 mt-2" loading={$authStore.loading}>
			{$authStore.loading ? 'Signing in…' : 'Sign in'}
		</Button>
	</form>
</AuthShell>
