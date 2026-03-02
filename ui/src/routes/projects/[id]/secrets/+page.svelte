<script lang="ts">
	import { page } from '$app/stores';
	import { api, type Secret, type App } from '$lib/api';
	import { onMount } from 'svelte';

	let secrets = $state<Secret[]>([]);
	let apps = $state<App[]>([]);
	let error = $state('');
	let showCreate = $state(false);
	let projectId = $derived($page.params.id ?? '');

	let newSecret = $state({ name: '', value: '', description: '' });

	let attachingSecretId = $state<string | null>(null);
	let attachAppId = $state('');
	let attachTarget = $state('');

	onMount(() => loadData());

	async function loadData() {
		try {
			[secrets, apps] = await Promise.all([
				api.listSecrets(projectId),
				api.listApps(projectId),
			]);
		} catch (e: any) {
			error = e.message;
		}
	}

	async function createSecret(e: Event) {
		e.preventDefault();
		try {
			const secret = await api.createSecret(projectId, newSecret);
			secrets = [secret, ...secrets];
			showCreate = false;
			newSecret = { name: '', value: '', description: '' };
		} catch (e: any) {
			error = e.message;
		}
	}

	async function deleteSecret(secretId: string) {
		if (!confirm('Delete this secret? It will be removed from Docker and all attached apps.')) return;
		try {
			await api.deleteSecret(projectId, secretId);
			secrets = secrets.filter(s => s.id !== secretId);
		} catch (e: any) {
			error = e.message;
		}
	}

	async function attachSecret(secretId: string) {
		if (!attachAppId) return;
		try {
			await api.attachSecret(projectId, secretId, attachAppId, {
				target: attachTarget || undefined,
			});
			attachingSecretId = null;
			attachAppId = '';
			attachTarget = '';
		} catch (e: any) {
			error = e.message;
		}
	}

	async function detachSecret(secretId: string, appId: string) {
		try {
			await api.detachSecret(projectId, secretId, appId);
		} catch (e: any) {
			error = e.message;
		}
	}

	function formatDate(d: string) {
		return new Date(d).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
	}
</script>

<div>
	<div class="mb-6">
		<a href="/projects/{projectId}" class="text-sm" style="color: var(--color-text-muted);">Back to project</a>
		<h2 class="text-2xl font-bold mt-1">Secrets</h2>
		<p class="text-sm mt-1" style="color: var(--color-text-muted);">Manage Docker Swarm secrets for this project. Secret values are stored securely in Docker and never exposed through the API.</p>
	</div>

	<div class="flex items-center justify-between mb-4">
		<span class="text-sm" style="color: var(--color-text-muted);">{secrets.length} secret{secrets.length !== 1 ? 's' : ''}</span>
		<button onclick={() => showCreate = !showCreate} class="px-3 py-1.5 rounded-lg text-sm font-medium cursor-pointer" style="background-color: var(--color-primary); color: var(--color-bg);">
			New Secret
		</button>
	</div>

	{#if showCreate}
		<form onsubmit={createSecret} class="rounded-lg p-4 mb-6 space-y-3" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<input type="text" bind:value={newSecret.name} placeholder="Secret name" required class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />
			<textarea bind:value={newSecret.value} placeholder="Secret value" required rows="3" class="w-full px-3 py-2 rounded-lg text-sm outline-none font-mono resize-y" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"></textarea>
			<input type="text" bind:value={newSecret.description} placeholder="Description (optional)" class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />
			<div class="flex gap-2">
				<button type="submit" class="px-4 py-2 rounded-lg text-sm font-medium cursor-pointer" style="background-color: var(--color-primary); color: var(--color-bg);">Create Secret</button>
				<button type="button" onclick={() => showCreate = false} class="px-4 py-2 rounded-lg text-sm cursor-pointer" style="color: var(--color-text-muted);">Cancel</button>
			</div>
		</form>
	{/if}

	<div class="space-y-3">
		{#each secrets as secret}
			<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<div class="flex items-center justify-between">
					<div>
						<span class="font-semibold font-mono">{secret.name}</span>
						{#if secret.description}
							<span class="text-sm ml-2" style="color: var(--color-text-muted);">{secret.description}</span>
						{/if}
						<div class="text-xs mt-1" style="color: var(--color-text-muted);">Created {formatDate(secret.created_at)}</div>
					</div>
					<div class="flex items-center gap-2">
						<button
							onclick={() => { attachingSecretId = attachingSecretId === secret.id ? null : secret.id; }}
							class="px-3 py-1.5 rounded-lg text-xs font-medium cursor-pointer"
							style="border: 1px solid var(--color-border); color: var(--color-text-muted);"
						>Attach to App</button>
						<button
							onclick={() => deleteSecret(secret.id)}
							class="px-3 py-1.5 rounded-lg text-xs font-medium cursor-pointer"
							style="border: 1px solid var(--color-danger); color: var(--color-danger);"
						>Delete</button>
					</div>
				</div>

				{#if attachingSecretId === secret.id}
					<div class="mt-3 p-3 rounded-lg space-y-2" style="background-color: var(--color-bg); border: 1px solid var(--color-border);">
						<select bind:value={attachAppId} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-surface); border: 1px solid var(--color-border); color: var(--color-text);">
							<option value="">Select an app...</option>
							{#each apps as app}
								<option value={app.id}>{app.name}</option>
							{/each}
						</select>
						<input type="text" bind:value={attachTarget} placeholder="Target filename (optional, defaults to secret name)" class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-surface); border: 1px solid var(--color-border); color: var(--color-text);" />
						<button onclick={() => attachSecret(secret.id)} class="px-3 py-1.5 rounded-lg text-xs font-medium cursor-pointer" style="background-color: var(--color-primary); color: var(--color-bg);">
							Attach
						</button>
					</div>
				{/if}
			</div>
		{/each}
		{#if secrets.length === 0}
			<p class="text-sm py-4" style="color: var(--color-text-muted);">No secrets in this project yet.</p>
		{/if}
	</div>

	{#if error}
		<div class="rounded-lg p-4 mt-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);">
			<p style="color: var(--color-danger);">{error}</p>
		</div>
	{/if}
</div>
