<script lang="ts">
	import { authClient } from '$lib/auth-client';
	import { goto } from '$app/navigation';

	let email = $state('');
	let password = $state('');
	let error = $state('');
	let loading = $state(false);

	async function handleLogin(e: Event) {
		e.preventDefault();
		loading = true;
		error = '';

		try {
			const result = await authClient.signIn.email({ email, password });
			if (result.error) {
				error = result.error.message || 'Login failed';
			} else {
				goto('/');
			}
		} catch (e: any) {
			error = e.message || 'Login failed';
		} finally {
			loading = false;
		}
	}
</script>

<div class="min-h-screen flex items-center justify-center p-4">
	<div class="w-full max-w-sm">
		<div class="text-center mb-8">
			<h1 class="text-3xl font-bold" style="color: var(--color-primary);">Hive</h1>
			<p class="mt-2 text-sm" style="color: var(--color-text-muted);">Sign in to your homelab</p>
		</div>

		<form onsubmit={handleLogin} class="space-y-4">
			{#if error}
				<div class="rounded-lg p-3 text-sm" style="background-color: rgba(239, 68, 68, 0.1); color: var(--color-danger);">
					{error}
				</div>
			{/if}

			<div>
				<label for="email" class="block text-sm font-medium mb-1.5" style="color: var(--color-text-muted);">Email</label>
				<input
					id="email"
					type="email"
					bind:value={email}
					required
					class="w-full px-3 py-2 rounded-lg text-sm outline-none transition-colors"
					style="background-color: var(--color-surface); border: 1px solid var(--color-border); color: var(--color-text);"
					placeholder="admin@homelab.local"
				/>
			</div>

			<div>
				<label for="password" class="block text-sm font-medium mb-1.5" style="color: var(--color-text-muted);">Password</label>
				<input
					id="password"
					type="password"
					bind:value={password}
					required
					class="w-full px-3 py-2 rounded-lg text-sm outline-none transition-colors"
					style="background-color: var(--color-surface); border: 1px solid var(--color-border); color: var(--color-text);"
				/>
			</div>

			<button
				type="submit"
				disabled={loading}
				class="w-full py-2.5 rounded-lg text-sm font-medium transition-colors cursor-pointer disabled:opacity-50"
				style="background-color: var(--color-primary); color: var(--color-bg);"
			>
				{loading ? 'Signing in...' : 'Sign in'}
			</button>
		</form>

		<p class="text-center mt-6 text-sm" style="color: var(--color-text-muted);">
			Don't have an account? <a href="/auth/register" class="font-medium" style="color: var(--color-primary);">Register</a>
		</p>
	</div>
</div>
