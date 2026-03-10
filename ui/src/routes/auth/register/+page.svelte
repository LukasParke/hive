<script lang="ts">
	import { HiveLogo, Button, Input } from '$lib/components';
	import { authClient } from '$lib/auth-client';
	import { goto } from '$app/navigation';

	let name = $state('');
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

	async function handleRegister(e: Event) {
		e.preventDefault();
		loading = true;
		error = '';

		try {
			const { error: signUpError } = await authClient.signUp.email({ name, email, password });
			if (signUpError) {
				console.error('[hive] Registration error:', signUpError);
				error = extractErrorMessage(signUpError, 'Registration failed. Please try again.');
			} else {
				goto('/');
			}
		} catch (e: any) {
			console.error('[hive] Registration exception:', e);
			error = e.message || 'An unexpected error occurred. Please try again.';
		} finally {
			loading = false;
		}
	}
</script>

<svelte:head><title>Register | Hive</title></svelte:head>

<div class="auth-page">
	<div class="w-full max-w-sm">
		<div class="text-center mb-8">
			<div class="flex justify-center mb-3">
				<HiveLogo size={48} />
			</div>
			<h1 class="text-3xl font-bold" style="color: var(--color-primary);">Hive</h1>
			<p class="mt-2 text-sm" style="color: var(--color-text-muted);">Create your account</p>
		</div>

		<form onsubmit={handleRegister} class="auth-card space-y-4">
			{#if error}
				<div class="alert alert-error">{error}</div>
			{/if}

			<Input label="Name" type="text" bind:value={name} required />
			<Input label="Email" type="email" bind:value={email} required />
			<Input label="Password" type="password" bind:value={password} required minlength={8} />

			<Button type="submit" variant="primary" {loading} class="w-full">
				{loading ? 'Creating account...' : 'Create account'}
			</Button>
		</form>

		<p class="text-center mt-6 text-sm" style="color: var(--color-text-muted);">
			Already have an account? <a href="/auth/login" class="font-medium" style="color: var(--color-primary);">Sign in</a>
		</p>
	</div>
</div>

<style>
	.auth-page {
		min-height: 100vh;
		display: flex;
		align-items: center;
		justify-content: center;
		padding: var(--space-md);
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
	}
	.auth-card::before {
		content: '';
		position: absolute;
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--color-primary);
		opacity: 0.8;
		offset-path: path('M0,0 L320,0 L320,480 L0,480 Z');
		animation: border-beam 8s linear infinite;
		filter: blur(3px);
		z-index: 1;
	}
</style>
