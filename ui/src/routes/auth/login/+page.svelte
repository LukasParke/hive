<script lang="ts">
	import { HiveLogo, Button, Input } from '$lib/components';
	import { authClient } from '$lib/auth-client';
	import { goto } from '$app/navigation';

	let email = $state('');
	let password = $state('');
	let error = $state('');
	let loading = $state(false);

	function extractErrorMessage(err: any, fallback: string): string {
		if (!err) return fallback;
		if (err.message && typeof err.message === 'string') return err.message;
		if (err.statusCode && err.statusCode >= 500)
			return `Server error (${err.statusCode}). Please check server logs.`;
		if (err.status && err.status >= 500)
			return `Server error (${err.status}). Please check server logs.`;
		if (err.code) return `${fallback} (${err.code})`;
		return fallback;
	}

	async function handleLogin(e: Event) {
		e.preventDefault();
		loading = true;
		error = '';

		try {
			const { error: signInError } = await authClient.signIn.email({ email, password });
			if (signInError) {
				error = extractErrorMessage(signInError, 'Login failed. Please check your credentials.');
			} else {
				goto('/');
			}
		} catch (e: any) {
			error = e.message || 'An unexpected error occurred. Please try again.';
		} finally {
			loading = false;
		}
	}
</script>

<svelte:head><title>Sign In | Hive</title></svelte:head>

<div class="auth-page">
	<div class="w-full max-w-sm">
		<div class="text-center mb-8">
			<div class="flex justify-center mb-3">
				<HiveLogo size={48} />
			</div>
			<h1 class="text-3xl font-bold text-primary">Hive</h1>
			<p class="mt-2 text-sm text-muted">Sign in to your homelab</p>
		</div>

		<form onsubmit={handleLogin} class="auth-card">
			{#if error}
				<div class="alert alert-error">
					{error}
				</div>
			{/if}

			<Input label="Email" type="email" bind:value={email} required placeholder="admin@homelab.local" />
			<Input label="Password" type="password" bind:value={password} required />

			<Button variant="primary" type="submit" {loading} class="w-full">
				{loading ? 'Signing in...' : 'Sign in'}
			</Button>
		</form>

		<p class="text-center mt-6 text-sm text-muted">
			Don't have an account? <a href="/auth/register" class="font-medium text-primary">Register</a>
		</p>
	</div>
</div>

<style>
	.auth-page {
		min-height: 100vh;
		display: flex;
		align-items: center;
		justify-content: center;
		padding: 1rem;
		background:
			radial-gradient(ellipse at center, rgba(229, 160, 13, 0.05) 0%, transparent 70%),
			var(--color-bg);
	}
	.auth-card {
		position: relative;
		background-color: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-xl);
		padding: var(--space-lg);
		box-shadow: 0 0 80px rgba(229, 160, 13, 0.06);
		overflow: hidden;
		display: flex;
		flex-direction: column;
		gap: var(--space-md);
	}
	.auth-card::before {
		content: '';
		position: absolute;
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--color-primary);
		opacity: 0.8;
		offset-path: path('M0,0 L320,0 L320,400 L0,400 Z');
		animation: border-beam 8s linear infinite;
		filter: blur(3px);
		z-index: 1;
	}
</style>
