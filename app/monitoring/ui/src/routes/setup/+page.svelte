<script lang="ts">
	import { goto } from '$app/navigation';
	import { base } from '$app/paths';
	import { authStore } from '$lib/stores/auth';
	import AuthShell from '$lib/components/AuthShell.svelte';
	import { Button, Field } from '$lib/components/ui';

	let email = $state('');
	let password = $state('');
	let confirmPassword = $state('');
	let localError = $state<string | null>(null);

	function validatePassword(): boolean {
		if (password.length < 8) {
			localError = 'Password must be at least 8 characters';
			return false;
		}
		if (password !== confirmPassword) {
			localError = 'Passwords do not match';
			return false;
		}
		localError = null;
		return true;
	}

	async function handleSubmit(e: Event) {
		e.preventDefault();

		if (!validatePassword()) {
			return;
		}

		const success = await authStore.setup(email, password, confirmPassword);
		if (success) {
			goto(base || '/');
		}
	}
</script>

<svelte:head>
	<title>Set up · runqy</title>
</svelte:head>

<AuthShell
	title="Welcome to runqy"
	subtitle="Create the admin account to get started."
	error={localError || $authStore.error}
	footnote="This is the only admin account of this runqy instance."
>
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

		<Field label="Password" for="password" hint="At least 8 characters.">
			<input
				id="password"
				type="password"
				class="input"
				autocomplete="new-password"
				bind:value={password}
				required
				minlength="8"
				disabled={$authStore.loading}
			/>
		</Field>

		<Field label="Confirm password" for="confirm-password">
			<input
				id="confirm-password"
				type="password"
				class="input"
				autocomplete="new-password"
				bind:value={confirmPassword}
				required
				minlength="8"
				disabled={$authStore.loading}
			/>
		</Field>

		<Button type="submit" variant="primary" class="w-full !h-9 mt-2" loading={$authStore.loading}>
			{$authStore.loading ? 'Creating account…' : 'Create admin account'}
		</Button>
	</form>
</AuthShell>
