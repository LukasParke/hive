<script lang="ts">
	import { api } from '$lib/api';
	import { invalidateAll } from '$app/navigation';
	import { Button, Card, Input, Modal, EmptyState } from '$lib/components';

	let { data } = $props();

	let showCreate = $state(false);
	let newName = $state('');
	let newDesc = $state('');
	let loading = $state(false);
	let deletingId = $state('');
	let success = $state('');
	let error = $state('');

	async function createProject(e: Event) {
		e.preventDefault();
		loading = true;
		error = '';
		try {
			await api.createProject({ name: newName, description: newDesc });
			showCreate = false;
			newName = '';
			newDesc = '';
			success = 'Project created.';
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		} finally {
			loading = false;
		}
	}

	async function deleteProject(id: string) {
		if (!confirm('Delete this project and all its apps?')) return;
		deletingId = id;
		try {
			await api.deleteProject(id);
			success = 'Project deleted.';
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		} finally {
			deletingId = '';
		}
	}
</script>

<svelte:head><title>Projects | Hive</title></svelte:head>

<div class="page-header">
	<div>
		<h2 class="page-title">Projects</h2>
		<p class="page-subtitle">{(data.projects ?? []).length} project{(data.projects ?? []).length !== 1 ? 's' : ''}</p>
	</div>
	<Button variant="primary" onclick={() => showCreate = true}>
		<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>
		New Project
	</Button>
</div>

{#if error}
	<div class="alert alert-error mb-4">
		<p class="text-danger text-sm">{error}</p>
	</div>
{/if}
{#if success}
	<div class="alert alert-success mb-4">
		<p class="text-sm">{success}</p>
	</div>
{/if}

<Modal bind:open={showCreate} title="Create Project">
	<form onsubmit={createProject} class="flex flex-col gap-4">
		<Input label="Project Name" bind:value={newName} placeholder="my-project" required />
		<Input label="Description" bind:value={newDesc} placeholder="Optional description" />
		{#snippet footer()}
			<Button variant="ghost" onclick={() => showCreate = false}>Cancel</Button>
			<Button variant="primary" loading={loading} type="submit">Create Project</Button>
		{/snippet}
	</form>
</Modal>

{#if (data.projects ?? []).length === 0}
	<EmptyState
		title="No projects yet"
		description="Create a project to start deploying apps."
	>
		{#snippet action()}
			<Button variant="primary" onclick={() => showCreate = true}>Create your first project</Button>
		{/snippet}
	</EmptyState>
{:else}
	<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
		{#each data.projects ?? [] as project}
			<a href="/projects/{project.id}" class="hive-card hive-card-interactive block" style="text-decoration: none; color: inherit;">
				<div class="hive-card-body">
					<div class="flex items-start justify-between">
						<div style="min-width: 0; flex: 1">
							<h3 style="font-weight: 600; font-size: var(--text-base)">{project.name}</h3>
							{#if project.description}
								<p class="text-muted" style="font-size: var(--text-sm); margin-top: var(--space-xs)">{project.description}</p>
							{/if}
						</div>
						<button
							onclick={(e) => { e.preventDefault(); e.stopPropagation(); deleteProject(project.id); }}
							class="btn btn-ghost btn-sm text-danger"
							disabled={deletingId === project.id}
						>{deletingId === project.id ? 'Deleting...' : 'Delete'}</button>
					</div>
					<p class="text-muted" style="font-size: var(--text-xs); margin-top: var(--space-md)">
						Created {new Date(project.created_at).toLocaleDateString()}
					</p>
				</div>
			</a>
		{/each}
	</div>
{/if}
