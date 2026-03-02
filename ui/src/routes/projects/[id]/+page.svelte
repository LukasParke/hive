<script lang="ts">
	import { page } from '$app/stores';
	import { api, type Project, type App, type ManagedDatabase } from '$lib/api';
	import { onMount } from 'svelte';

	let project = $state<Project | null>(null);
	let apps = $state<App[]>([]);
	let databases = $state<ManagedDatabase[]>([]);
	let error = $state('');
	let showCreateApp = $state(false);

	let newApp = $state({
		name: '',
		deploy_type: 'image',
		image: '',
		git_repo: '',
		domain: '',
		port: 3000,
	});

	$effect(() => {
		const id = $page.params.id;
		if (id) loadProject(id);
	});

	async function loadProject(id: string) {
		try {
			[project, apps, databases] = await Promise.all([
				api.getProject(id),
				api.listApps(id),
				api.listDatabases(id),
			]);
		} catch (e: any) {
			error = e.message;
		}
	}

	async function createApp(e: Event) {
		e.preventDefault();
		if (!project) return;
		try {
			const app = await api.createApp(project.id, newApp);
			apps = [app, ...apps];
			showCreateApp = false;
			newApp = { name: '', deploy_type: 'image', image: '', git_repo: '', domain: '', port: 3000 };
		} catch (e: any) {
			error = e.message;
		}
	}

	async function deployApp(appId: string) {
		if (!project) return;
		await api.deployApp(project.id, appId);
		if (project) apps = await api.listApps(project.id);
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

<div>
	{#if project}
		<div class="mb-6">
			<a href="/projects" class="text-sm" style="color: var(--color-text-muted);">Projects /</a>
			<h2 class="text-2xl font-bold mt-1">{project.name}</h2>
			{#if project.description}
				<p class="text-sm mt-1" style="color: var(--color-text-muted);">{project.description}</p>
			{/if}
		</div>

		<div class="flex gap-3 mb-6">
			<a href="/projects/{project.id}/secrets" class="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium" style="background-color: var(--color-surface); border: 1px solid var(--color-border); color: var(--color-text);">
				Secrets
			</a>
			<a href="/projects/{project.id}/volumes" class="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium" style="background-color: var(--color-surface); border: 1px solid var(--color-border); color: var(--color-text);">
				Volumes
			</a>
			<a href="/projects/{project.id}/stacks" class="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium" style="background-color: var(--color-surface); border: 1px solid var(--color-border); color: var(--color-text);">
				Stacks
			</a>
		</div>

		<div class="flex items-center justify-between mb-4">
			<h3 class="text-lg font-semibold">Apps</h3>
			<button onclick={() => showCreateApp = !showCreateApp} class="px-3 py-1.5 rounded-lg text-sm font-medium cursor-pointer" style="background-color: var(--color-primary); color: var(--color-bg);">
				New App
			</button>
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

				<div class="grid grid-cols-2 gap-3">
					<input type="text" bind:value={newApp.domain} placeholder="Domain (optional)" class="px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />
					<input type="number" bind:value={newApp.port} placeholder="Port" class="px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />
				</div>

				<div class="flex gap-2">
					<button type="submit" class="px-4 py-2 rounded-lg text-sm font-medium cursor-pointer" style="background-color: var(--color-primary); color: var(--color-bg);">Create</button>
					<button type="button" onclick={() => showCreateApp = false} class="px-4 py-2 rounded-lg text-sm cursor-pointer" style="color: var(--color-text-muted);">Cancel</button>
				</div>
			</form>
		{/if}

		<div class="space-y-3 mb-8">
			{#each apps as app}
				<div class="rounded-lg p-4 flex items-center justify-between" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
					<div>
						<a href="/projects/{project.id}/apps/{app.id}" class="font-semibold hover:underline">{app.name}</a>
						<a href="/projects/{project.id}/apps/{app.id}/logs" class="text-xs ml-2" style="color: var(--color-primary);">Logs</a>
						<div class="flex items-center gap-3 mt-1">
							<span class="text-xs px-2 py-0.5 rounded" style="background-color: var(--color-bg); color: var(--color-text-muted);">{app.deploy_type}</span>
							<span class="text-xs font-medium" style="color: {statusColor(app.status)};">{app.status}</span>
							{#if app.domain}
								<span class="text-xs" style="color: var(--color-text-muted);">{app.domain}</span>
							{/if}
						</div>
					</div>
					<div class="flex gap-2">
						<a href="/projects/{project.id}/apps/{app.id}/env" class="px-3 py-1.5 rounded-lg text-xs font-medium cursor-pointer" style="border: 1px solid var(--color-border); color: var(--color-text-muted);">
							Env
						</a>
						<button onclick={() => deployApp(app.id)} class="px-3 py-1.5 rounded-lg text-xs font-medium cursor-pointer" style="background-color: var(--color-primary); color: var(--color-bg);">
							Deploy
						</button>
					</div>
				</div>
			{/each}
			{#if apps.length === 0}
				<p class="text-sm py-4" style="color: var(--color-text-muted);">No apps in this project yet.</p>
			{/if}
		</div>

		<h3 class="text-lg font-semibold mb-4">Databases</h3>
		<div class="space-y-3">
			{#each databases as db}
				<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
					<div class="flex items-center gap-3">
						<span class="font-semibold">{db.name}</span>
						<span class="text-xs px-2 py-0.5 rounded" style="background-color: var(--color-bg); color: var(--color-text-muted);">{db.db_type}</span>
						<span class="text-xs" style="color: var(--color-text-muted);">{db.version}</span>
					</div>
				</div>
			{/each}
			{#if databases.length === 0}
				<p class="text-sm py-4" style="color: var(--color-text-muted);">No managed databases in this project yet.</p>
			{/if}
		</div>
	{/if}

	{#if error}
		<div class="rounded-lg p-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);">
			<p style="color: var(--color-danger);">{error}</p>
		</div>
	{/if}
</div>
