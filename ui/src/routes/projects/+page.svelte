<script lang="ts">
	import { api, type Project } from '$lib/api';
	import { onMount } from 'svelte';

	let projects = $state<Project[]>([]);
	let showCreate = $state(false);
	let newName = $state('');
	let newDesc = $state('');
	let loading = $state(false);
	let error = $state('');

	onMount(async () => {
		try {
			projects = await api.listProjects();
		} catch (e: any) {
			error = e.message;
		}
	});

	async function createProject(e: Event) {
		e.preventDefault();
		loading = true;
		try {
			const project = await api.createProject({ name: newName, description: newDesc });
			projects = [project, ...projects];
			showCreate = false;
			newName = '';
			newDesc = '';
		} catch (e: any) {
			error = e.message;
		} finally {
			loading = false;
		}
	}

	async function deleteProject(id: string) {
		await api.deleteProject(id);
		projects = projects.filter(p => p.id !== id);
	}
</script>

<div>
	<div class="flex items-center justify-between mb-6">
		<h2 class="text-2xl font-bold">Projects</h2>
		<button
			onclick={() => showCreate = !showCreate}
			class="px-4 py-2 rounded-lg text-sm font-medium cursor-pointer"
			style="background-color: var(--color-primary); color: var(--color-bg);"
		>
			New Project
		</button>
	</div>

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);">
			<p style="color: var(--color-danger);">{error}</p>
		</div>
	{/if}

	{#if showCreate}
		<form onsubmit={createProject} class="rounded-lg p-4 mb-6 space-y-3" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<input
				type="text"
				bind:value={newName}
				placeholder="Project name"
				required
				class="w-full px-3 py-2 rounded-lg text-sm outline-none"
				style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
			/>
			<input
				type="text"
				bind:value={newDesc}
				placeholder="Description (optional)"
				class="w-full px-3 py-2 rounded-lg text-sm outline-none"
				style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
			/>
			<div class="flex gap-2">
				<button type="submit" disabled={loading} class="px-4 py-2 rounded-lg text-sm font-medium cursor-pointer" style="background-color: var(--color-primary); color: var(--color-bg);">
					{loading ? 'Creating...' : 'Create'}
				</button>
				<button type="button" onclick={() => showCreate = false} class="px-4 py-2 rounded-lg text-sm cursor-pointer" style="color: var(--color-text-muted);">Cancel</button>
			</div>
		</form>
	{/if}

	<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
		{#each projects as project}
			<a href="/projects/{project.id}" class="rounded-lg p-4 transition-colors block" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<div class="flex items-start justify-between">
					<div>
						<h3 class="font-semibold">{project.name}</h3>
						{#if project.description}
							<p class="text-sm mt-1" style="color: var(--color-text-muted);">{project.description}</p>
						{/if}
					</div>
					<button
						onclick={(e) => { e.stopPropagation(); deleteProject(project.id); }}
						class="text-xs px-2 py-1 rounded cursor-pointer"
						style="color: var(--color-danger);"
					>Delete</button>
				</div>
				<p class="text-xs mt-3" style="color: var(--color-text-muted);">
					Created {new Date(project.created_at).toLocaleDateString()}
				</p>
			</a>
		{/each}
	</div>

	{#if projects.length === 0 && !showCreate}
		<div class="text-center py-12" style="color: var(--color-text-muted);">
			<p class="text-lg mb-2">No projects yet</p>
			<p class="text-sm">Create a project to start deploying apps.</p>
		</div>
	{/if}
</div>
