<script lang="ts">
	import { api, type Stack } from '$lib/api';
	import { page } from '$app/stores';
	import { onMount } from 'svelte';

	let projectId = $derived($page.params.id);
	let stacks = $state<Stack[]>([]);
	let showNew = $state(false);
	let newName = $state('');
	let newCompose = $state('');
	let error = $state('');
	let editingId = $state('');
	let editContent = $state('');

	onMount(async () => {
		await loadStacks();
	});

	async function loadStacks() {
		try {
			stacks = await api.listStacks(projectId);
		} catch (e: any) {
			error = e.message;
		}
	}

	async function createStack() {
		try {
			await api.createStack(projectId, { name: newName, compose_content: newCompose });
			newName = '';
			newCompose = '';
			showNew = false;
			await loadStacks();
		} catch (e: any) {
			error = e.message;
		}
	}

	async function updateStack(id: string) {
		try {
			await api.updateStack(projectId, id, { compose_content: editContent });
			editingId = '';
			await loadStacks();
		} catch (e: any) {
			error = e.message;
		}
	}

	async function deleteStack(id: string) {
		try {
			await api.deleteStack(projectId, id);
			await loadStacks();
		} catch (e: any) {
			error = e.message;
		}
	}
</script>

<div class="max-w-5xl mx-auto">
	<div class="flex items-center justify-between mb-6">
		<h2 class="text-2xl font-bold">Stacks</h2>
		<button onclick={() => showNew = !showNew} class="px-4 py-2 rounded-lg text-sm font-medium" style="background-color: #3b82f6; color: white;">
			{showNew ? 'Cancel' : 'New Stack'}
		</button>
	</div>

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid #ef4444;">
			<p style="color: #ef4444;">{error}</p>
		</div>
	{/if}

	{#if showNew}
		<div class="rounded-lg p-6 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="text-lg font-semibold mb-4">Deploy New Stack</h3>
			<div class="space-y-4">
				<input bind:value={newName} placeholder="Stack name" class="w-full px-3 py-2 rounded-lg text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				<textarea bind:value={newCompose} placeholder="Paste your docker-compose.yml content here..." rows="15" class="w-full px-3 py-2 rounded-lg text-sm font-mono" style="background-color: var(--color-bg); border: 1px solid var(--color-border);"></textarea>
				<button onclick={createStack} class="px-4 py-2 rounded-lg text-sm font-medium" style="background-color: #22c55e; color: white;">Deploy Stack</button>
			</div>
		</div>
	{/if}

	<div class="space-y-4">
		{#each stacks as stack}
			<div class="rounded-lg p-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<div class="flex items-center justify-between mb-4">
					<div>
						<h3 class="font-semibold text-lg">{stack.name}</h3>
						<span class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium" style="background-color: {stack.status === 'running' ? 'rgba(34,197,94,0.15)' : 'rgba(245,158,11,0.15)'}; color: {stack.status === 'running' ? '#22c55e' : '#f59e0b'};">
							{stack.status}
						</span>
					</div>
					<div class="flex gap-2">
						<button onclick={() => { editingId = editingId === stack.id ? '' : stack.id; editContent = stack.compose_content; }} class="px-3 py-1 rounded text-sm" style="border: 1px solid var(--color-border);">
							{editingId === stack.id ? 'Cancel' : 'Edit'}
						</button>
						<button onclick={() => deleteStack(stack.id)} class="px-3 py-1 rounded text-sm" style="color: #ef4444; border: 1px solid #ef4444;">
							Remove
						</button>
					</div>
				</div>

				{#if editingId === stack.id}
					<div class="space-y-3">
						<textarea bind:value={editContent} rows="12" class="w-full px-3 py-2 rounded-lg text-sm font-mono" style="background-color: var(--color-bg); border: 1px solid var(--color-border);"></textarea>
						<button onclick={() => updateStack(stack.id)} class="px-4 py-2 rounded-lg text-sm font-medium" style="background-color: #f59e0b; color: white;">Update & Redeploy</button>
					</div>
				{/if}
			</div>
		{:else}
			<div class="text-center py-12" style="color: var(--color-text-muted);">
				<p class="text-lg mb-2">No stacks deployed</p>
				<p class="text-sm">Create a stack from a Docker Compose file</p>
			</div>
		{/each}
	</div>
</div>
