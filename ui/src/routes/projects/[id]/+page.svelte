<script lang="ts">
	import { api } from '$lib/api';
	import { Button, Badge, EmptyState, Input, Modal } from '$lib/components';
	import { invalidateAll } from '$app/navigation';

	let { data } = $props();

	let error = $state('');
	let success = $state('');
	let showCreateApp = $state(false);
	let deleteTarget = $state<{ id: string; name: string } | null>(null);
	let actionLoading = $state('');

	let newApp = $state({
		name: '',
		deploy_type: 'image',
		image: '',
		git_repo: '',
		domain: '',
		port: 3000,
	});

	async function createApp(e: Event) {
		e.preventDefault();
		if (!data.project) return;
		actionLoading = 'create';
		error = '';
		success = '';
		try {
			await api.createApp(data.project.id, newApp);
			showCreateApp = false;
			newApp = { name: '', deploy_type: 'image', image: '', git_repo: '', domain: '', port: 3000 };
			success = 'App created.';
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		} finally {
			actionLoading = '';
		}
	}

	async function deployApp(appId: string) {
		if (!data.project) return;
		actionLoading = `deploy:${appId}`;
		error = '';
		success = '';
		try {
			await api.deployApp(data.project.id, appId);
			success = 'Deploy triggered.';
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		} finally {
			actionLoading = '';
		}
	}

	async function deleteApp() {
		if (!data.project || !deleteTarget) return;
		actionLoading = `delete:${deleteTarget.id}`;
		error = '';
		success = '';
		try {
			await api.deleteApp(data.project.id, deleteTarget.id);
			deleteTarget = null;
			success = 'App deleted.';
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
			deleteTarget = null;
		} finally {
			actionLoading = '';
		}
	}

	function statusColor(status: string): string {
		switch (status) {
			case 'running': return 'var(--color-success)';
			case 'deploying': return 'var(--color-warning)';
			case 'failed': return 'var(--color-danger)';
			default: return 'var(--color-text-muted)';
		}
	}
</script>

<svelte:head><title>{data.project?.name ?? 'Project'} | Hive</title></svelte:head>

<div>
	{#if data.project}
		<div class="page-header">
			<div>
				<div class="flex items-center gap-2 mb-1">
					<a href="/projects" class="text-sm" style="color: var(--color-text-muted);">Projects /</a>
				</div>
				<h2 class="page-title">{data.project.name}</h2>
				{#if data.project.description}
					<p class="page-subtitle">{data.project.description}</p>
				{/if}
			</div>
		</div>

		<div class="flex gap-2 flex-wrap mb-6">
			<a href="/projects/{data.project.id}/secrets" class="project-tab-link">
				Secrets
			</a>
			<a href="/projects/{data.project.id}/volumes" class="project-tab-link">
				Volumes
			</a>
			<a href="/projects/{data.project.id}/stacks" class="project-tab-link">
				Stacks
			</a>
		</div>

		<div class="flex items-center justify-between mb-4">
			<h3 class="text-lg font-semibold">Apps</h3>
			<Button variant="primary" size="sm" onclick={() => showCreateApp = !showCreateApp}>
				New App
			</Button>
		</div>

		{#if showCreateApp}
			<form onsubmit={createApp} class="rounded-lg p-4 mb-6 space-y-3" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<input type="text" bind:value={newApp.name} placeholder="App name" required class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />

				<select bind:value={newApp.deploy_type} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);">
					<option value="image">Docker Image</option>
					<option value="git">Git Repository</option>
					<option value="compose">Docker Compose</option>
				</select>

				{#if newApp.deploy_type === 'image'}
					<input type="text" bind:value={newApp.image} placeholder="Image (e.g. nginx:latest)" class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />
				{:else if newApp.deploy_type === 'git'}
					<input type="text" bind:value={newApp.git_repo} placeholder="Repository URL" class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />
				{/if}

				<div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
					<input type="text" bind:value={newApp.domain} placeholder="Domain (optional)" class="px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />
					<input type="number" bind:value={newApp.port} placeholder="Port" class="px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />
				</div>

				<div class="flex gap-2">
					<button type="submit" class="px-4 py-2 rounded-lg text-sm font-medium cursor-pointer" style="background-color: var(--color-primary); color: var(--color-bg);" disabled={actionLoading === 'create'}>
						{actionLoading === 'create' ? 'Creating...' : 'Create'}
					</button>
					<button type="button" onclick={() => showCreateApp = false} class="px-4 py-2 rounded-lg text-sm cursor-pointer" style="color: var(--color-text-muted);">Cancel</button>
				</div>
			</form>
		{/if}

		<div class="space-y-3 mb-8">
			{#each data.apps ?? [] as app}
				<div class="rounded-lg p-4 flex flex-col sm:flex-row sm:items-center justify-between gap-3" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
					<div class="min-w-0">
						<a href="/projects/{data.project.id}/apps/{app.id}" class="font-semibold hover:underline">{app.name}</a>
						<a href="/projects/{data.project.id}/apps/{app.id}/logs" class="text-xs ml-2" style="color: var(--color-primary);">Logs</a>
					<div class="flex items-center gap-3 mt-1 flex-wrap">
						<span class="text-xs px-2 py-0.5 rounded" style="background-color: var(--color-bg); color: var(--color-text-muted);">{app.deploy_type}</span>
						<span class="text-xs font-medium" style="color: {statusColor(app.status)};">{app.status}</span>
						{#if app.domain}
							<a href="https://{app.domain}" target="_blank" class="text-xs underline truncate" style="color: var(--color-primary);">{app.domain}</a>
						{/if}
						{#if app.port}
							<span class="text-xs font-mono" style="color: var(--color-text-muted);">:{app.port}</span>
						{/if}
					</div>
					</div>
					<div class="flex gap-2 shrink-0">
						<a href="/projects/{data.project.id}/apps/{app.id}/env" class="app-card-btn" style="border: 1px solid var(--color-border); color: var(--color-text-muted);">
							Env
						</a>
					<button onclick={() => deployApp(app.id)} class="app-card-btn" style="background-color: var(--color-primary); color: var(--color-bg);" disabled={actionLoading === `deploy:${app.id}`}>
						{actionLoading === `deploy:${app.id}` ? 'Deploying...' : app.status === 'failed' ? 'Retry' : 'Deploy'}
					</button>
						<button onclick={() => deleteTarget = { id: app.id, name: app.name }} class="app-card-btn" style="border: 1px solid rgba(239,68,68,0.3); color: var(--color-danger);">
							Delete
						</button>
					</div>
				</div>
			{/each}
			{#if (data.apps ?? []).length === 0}
				<EmptyState title="No apps yet" description="Create your first app to get started." />
			{/if}
		</div>

		<h3 class="text-lg font-semibold mb-4">Databases</h3>
		<div class="space-y-3">
			{#each data.databases ?? [] as db}
				<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
					<div class="flex items-center gap-3">
						<span class="font-semibold">{db.name}</span>
						<span class="text-xs px-2 py-0.5 rounded" style="background-color: var(--color-bg); color: var(--color-text-muted);">{db.db_type}</span>
						<span class="text-xs" style="color: var(--color-text-muted);">{db.version}</span>
					</div>
				</div>
			{/each}
			{#if (data.databases ?? []).length === 0}
				<EmptyState title="No databases" description="No managed databases in this project yet." />
			{/if}
		</div>
	{/if}

	{#if error}
		<div class="alert alert-error">
			<p style="color: var(--color-danger);">{error}</p>
		</div>
	{/if}
	{#if success}
		<div class="alert alert-success">
			<p>{success}</p>
		</div>
	{/if}
</div>

{#if deleteTarget}
	<Modal open={true} title="Delete App" onclose={() => deleteTarget = null}>
		<p class="text-sm mb-4">Are you sure you want to delete <strong>{deleteTarget.name}</strong>? This will remove the Docker service and all associated data. This action cannot be undone.</p>
		<div class="flex gap-2 justify-end">
			<Button variant="ghost" size="sm" onclick={() => deleteTarget = null}>Cancel</Button>
			<Button variant="danger" size="sm" onclick={deleteApp} loading={actionLoading.startsWith('delete:')}>Delete App</Button>
		</div>
	</Modal>
{/if}

<style>
	.project-tab-link {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.5rem 1rem;
		border-radius: var(--radius-lg);
		font-size: var(--text-sm);
		font-weight: 500;
		background-color: var(--color-surface);
		border: 1px solid var(--color-border);
		color: var(--color-text);
		text-decoration: none;
		transition: all var(--transition-base);
	}
	.project-tab-link:hover {
		border-color: var(--color-primary-border);
		background-color: var(--color-surface-hover);
	}
	.app-card-btn {
		padding: 0.375rem 0.75rem;
		border-radius: var(--radius-lg);
		font-size: var(--text-xs);
		font-weight: 500;
		cursor: pointer;
		white-space: nowrap;
		transition: all var(--transition-base);
	}

	@media (max-width: 768px) {
		.project-tab-link {
			min-height: 44px;
			flex: 1;
			justify-content: center;
		}
		.app-card-btn {
			min-height: 40px;
			padding: 0.5rem 0.875rem;
			font-size: var(--text-sm);
		}
	}
</style>
