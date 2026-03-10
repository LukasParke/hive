<script lang="ts">
	import { api } from '$lib/api';
	import { invalidateAll } from '$app/navigation';
	import type { APIToken } from '$lib/types';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();

	let showForm = $state(false);
	let saving = $state(false);
	let error = $state<string | null>(null);
	let revealedToken = $state<string | null>(null);
	let revokeId = $state<string | null>(null);

	const SCOPES = [
		{ id: 'read', label: 'Read' },
		{ id: 'write', label: 'Write' },
		{ id: 'admin', label: 'Admin' },
	];

	let form = $state({
		name: '',
		scopes: ['read'] as string[],
		expires_in_days: 30,
	});

	function resetForm() {
		form = { name: '', scopes: ['read'], expires_in_days: 30 };
		showForm = false;
		error = null;
		revealedToken = null;
	}

	function toggleScope(id: string) {
		if (form.scopes.includes(id)) {
			form = { ...form, scopes: form.scopes.filter((s) => s !== id) };
		} else {
			form = { ...form, scopes: [...form.scopes, id] };
		}
	}

	async function submitForm() {
		if (form.scopes.length === 0) {
			error = 'Select at least one scope';
			return;
		}
		saving = true;
		error = null;
		try {
			const res = await api.createToken({
				name: form.name,
				scopes: form.scopes,
				expires_in_days: form.expires_in_days || undefined,
			});
			revealedToken = (res as { token?: string }).token ?? null;
			if (!revealedToken) {
				resetForm();
				await invalidateAll();
			}
		} catch (e: unknown) {
			error = e instanceof Error ? e.message : 'Failed to create token';
		} finally {
			saving = false;
		}
	}

	async function copyAndClose() {
		if (revealedToken) {
			await navigator.clipboard.writeText(revealedToken);
		}
		resetForm();
		await invalidateAll();
	}

	async function revokeToken(id: string) {
		revokeId = id;
		try {
			await api.deleteToken(id);
			await invalidateAll();
		} catch (e: unknown) {
			alert(e instanceof Error ? e.message : 'Failed to revoke');
		} finally {
			revokeId = null;
		}
	}

	function formatDate(d: string | null) {
		return d ? new Date(d).toLocaleString() : 'Never';
	}

	function parseScopes(s: string): string[] {
		if (!s) return [];
		try {
			const v = JSON.parse(s);
			return Array.isArray(v) ? v : [String(s)];
		} catch {
			return s.split(',').map((x) => x.trim()).filter(Boolean);
		}
	}

	const tokens = $derived((data?.data ?? []) as APIToken[]);
</script>

<svelte:head><title>API Tokens | Hive</title></svelte:head>

<div class="max-w-4xl mx-auto">
	<div class="page-header">
		<div>
			<h2 class="page-title">API Tokens</h2>
			<p class="page-subtitle">Create and manage tokens for programmatic API access</p>
		</div>
		<button
			class="btn btn-primary"
			onclick={() => (showForm ? resetForm() : (showForm = true))}
		>
			{showForm ? 'Cancel' : '+ Create Token'}
		</button>
	</div>

	{#if showForm && !revealedToken}
		<form
			onsubmit={(e) => {
				e.preventDefault();
				submitForm();
			}}
			class="rounded-lg p-6 mb-6 bg-slate-800/50 border border-slate-700 space-y-4"
		>
			<h3 class="text-lg font-semibold text-slate-200">Create Token</h3>
			{#if error}
				<div class="text-sm text-red-400 bg-red-900/20 px-3 py-2 rounded">
					{error}
				</div>
			{/if}
			<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
				<div>
					<label for="token-name">Name</label>
					<input id="token-name" type="text" bind:value={form.name} required placeholder="CI/CD pipeline" />
				</div>
				<div>
					<label for="token-expiry">Expires in (days)</label>
					<input id="token-expiry" type="number" bind:value={form.expires_in_days} min="0" placeholder="0 = never" />
				</div>
			</div>
			<fieldset>
				<legend class="text-sm font-medium text-slate-300 mb-2">Scopes</legend>
				<div class="flex gap-4 flex-wrap">
					{#each SCOPES as scope}
						<label class="flex items-center gap-2 cursor-pointer text-slate-300">
							<input
								type="checkbox"
								checked={form.scopes.includes(scope.id)}
								onchange={() => toggleScope(scope.id)}
							/>
							{scope.label}
						</label>
					{/each}
				</div>
			</fieldset>
			<div class="flex justify-end gap-3">
				<button type="button" class="btn bg-slate-700 text-slate-200 border-slate-600" onclick={resetForm}>
					Cancel
				</button>
				<button type="submit" class="btn btn-primary" disabled={saving}>
					{saving ? 'Creating...' : 'Create'}
				</button>
			</div>
		</form>
	{/if}

	{#if revealedToken}
		<div class="rounded-lg p-6 mb-6 bg-amber-900/20 border border-amber-700">
			<h3 class="text-lg font-semibold text-amber-200 mb-2">Token created – copy it now</h3>
			<p class="text-sm text-slate-400 mb-3">This token will only be shown once. Store it securely.</p>
			<div class="flex gap-2">
				<code class="flex-1 px-3 py-2 rounded bg-slate-900 text-slate-300 text-sm font-mono break-all">
					{revealedToken}
				</code>
				<button class="btn bg-slate-700 text-slate-200 shrink-0" onclick={() => navigator.clipboard.writeText(revealedToken!)}>
					Copy
				</button>
			</div>
			<button class="btn btn-primary mt-4" onclick={copyAndClose}>
				I've copied it
			</button>
		</div>
	{/if}

	{#if tokens.length === 0 && !showForm}
		<div class="rounded-lg p-8 text-center bg-slate-800/50 border border-slate-700">
			<p class="text-lg font-medium text-slate-200 mb-2">No API tokens</p>
			<p class="text-sm text-slate-400 mb-4">Create a token to authenticate scripts, CI/CD, or integrations.</p>
			<button class="btn btn-primary" onclick={() => (showForm = true)}>
				+ Create Token
			</button>
		</div>
	{:else}
		<div class="space-y-4">
			{#each tokens as token}
				<div class="rounded-lg p-5 bg-slate-800/50 border border-slate-700">
					<div class="flex items-start justify-between gap-4">
						<div class="min-w-0 flex-1">
							<h3 class="font-semibold text-slate-200">{token.name}</h3>
							<div class="flex flex-wrap gap-2 mt-2">
								{#each parseScopes(token.scopes) as scope}
									<span class="text-xs px-2 py-0.5 rounded bg-slate-600 text-slate-300">
										{scope}
									</span>
								{/each}
							</div>
							<div class="text-xs text-slate-500 mt-2 space-x-4">
								<span>Created {formatDate(token.created_at)}</span>
								{#if token.last_used_at}
									<span>Last used {formatDate(token.last_used_at)}</span>
								{/if}
								{#if token.expires_at}
									<span>Expires {formatDate(token.expires_at)}</span>
								{:else}
									<span>Never expires</span>
								{/if}
							</div>
						</div>
						<button
							class="btn bg-red-900/40 text-red-400 border-red-800 hover:bg-red-900/60"
							disabled={revokeId === token.id}
							onclick={() => {
								if (confirm('Revoke this token? It will stop working immediately.')) {
									revokeToken(token.id);
								}
							}}
						>
							{revokeId === token.id ? 'Revoking...' : 'Revoke'}
						</button>
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>
