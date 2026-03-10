<script lang="ts">
	import { api } from '$lib/api';
	import { invalidateAll } from '$app/navigation';

	let { data } = $props();

	let showNew = $state(false);
	let newName = $state('');
	let newDomain = $state('');
	let newCompose = $state('');
	let error = $state('');
	let success = $state('');
	let busy = $state('');
	let editingId = $state('');
	let editContent = $state('');
	let editDomain = $state('');
	let stackServices = $state<Record<string, { name: string; replicas: number; running: number; healthy: boolean; image: string }[]>>({});
	let loadingServices = $state<Record<string, boolean>>({});

	async function loadStackServices(stackId: string) {
		if (stackServices[stackId]) return;
		loadingServices[stackId] = true;
		try {
			const svcs = await api.getStackServices(data.projectId, stackId);
			stackServices[stackId] = svcs;
		} catch (e: any) {
			stackServices[stackId] = [];
		}
		loadingServices[stackId] = false;
	}

	$effect(() => {
		for (const stack of data.stacks ?? []) {
			loadStackServices(stack.id);
		}
	});

	async function createStack() {
		busy = 'create';
		error = '';
		success = '';
		try {
			await api.createStack(data.projectId, { name: newName, compose_content: newCompose, domain: newDomain || undefined });
			newName = '';
			newDomain = '';
			newCompose = '';
			showNew = false;
			success = 'Stack deploy submitted.';
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		} finally {
			busy = '';
		}
	}

	async function updateStack(id: string) {
		busy = `update:${id}`;
		error = '';
		success = '';
		try {
			await api.updateStack(data.projectId, id, { compose_content: editContent, domain: editDomain });
			editingId = '';
			success = 'Stack updated and redeployed.';
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		} finally {
			busy = '';
		}
	}

	async function deleteStack(id: string) {
		if (!confirm('Remove this stack? All services and data will be deleted.')) return;
		busy = `delete:${id}`;
		error = '';
		success = '';
		try {
			await api.deleteStack(data.projectId, id);
			success = 'Stack deleted.';
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		} finally {
			busy = '';
		}
	}

	function statusColor(status: string): string {
		switch (status) {
			case 'running': return 'var(--color-success)';
			case 'deploying':
			case 'pending':
				return 'var(--color-warning)';
			case 'degraded':
				return 'rgb(249, 115, 22)';
			case 'failed':
			case 'stopped':
				return 'var(--color-danger)';
			default: return 'var(--color-text-muted)';
		}
	}

	function statusBg(status: string): string {
		switch (status) {
			case 'running': return 'rgba(34, 197, 94, 0.12)';
			case 'deploying':
			case 'pending':
				return 'rgba(229, 160, 13, 0.12)';
			case 'degraded':
				return 'rgba(249, 115, 22, 0.12)';
			case 'failed':
			case 'stopped':
				return 'rgba(239, 68, 68, 0.12)';
			default: return 'rgba(154, 145, 138, 0.12)';
		}
	}
</script>

<svelte:head><title>Stacks | Hive</title></svelte:head>

<div class="max-w-5xl mx-auto">
	<div class="flex items-center justify-between mb-6">
		<h2 class="text-2xl font-bold">Stacks</h2>
		<button onclick={() => showNew = !showNew} class="px-4 py-2 rounded-lg text-sm font-medium" style="background-color: var(--color-primary); color: var(--color-bg);">
			{showNew ? 'Cancel' : 'New Stack'}
		</button>
	</div>

	{#if error}
		<div class="alert alert-error mb-4">
			<p style="color: var(--color-danger);">{error}</p>
		</div>
	{/if}
	{#if success}
		<div class="alert alert-success mb-4">
			<p>{success}</p>
		</div>
	{/if}

	{#if showNew}
		<div class="rounded-lg p-6 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="text-lg font-semibold mb-4">Deploy New Stack</h3>
			<div class="space-y-4">
				<input bind:value={newName} placeholder="Stack name" class="w-full px-3 py-2 rounded-lg text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				<input bind:value={newDomain} placeholder="Optional domain (example.com)" class="w-full px-3 py-2 rounded-lg text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				<textarea bind:value={newCompose} placeholder="Paste your docker-compose.yml content here..." rows="15" class="w-full px-3 py-2 rounded-lg text-sm font-mono" style="background-color: var(--color-bg); border: 1px solid var(--color-border);"></textarea>
				<button onclick={createStack} class="px-4 py-2 rounded-lg text-sm font-medium" style="background-color: var(--color-success); color: white;" disabled={busy === 'create'}>
					{busy === 'create' ? 'Deploying...' : 'Deploy Stack'}
				</button>
			</div>
		</div>
	{/if}

	<div class="space-y-4">
		{#each data.stacks ?? [] as stack}
			<div class="rounded-lg p-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<div class="flex items-center justify-between mb-4">
					<div class="flex items-center gap-3">
						<h3 class="font-semibold text-lg">{stack.name}</h3>
						{#if stack.domain}
							<span class="text-xs" style="color: var(--color-text-muted);">{stack.domain}</span>
						{/if}
						<span
							class="inline-flex items-center px-2 py-0.5 rounded text-xs font-semibold uppercase"
							style="background-color: {statusBg(stack.status)}; color: {statusColor(stack.status)};"
						>
							{stack.status}
						</span>
					</div>
					<div class="flex gap-2">
						<button onclick={() => { editingId = editingId === stack.id ? '' : stack.id; editContent = stack.compose_content; editDomain = stack.domain || ''; }} class="px-3 py-1 rounded text-sm" style="border: 1px solid var(--color-border);">
							{editingId === stack.id ? 'Cancel' : 'Edit'}
						</button>
						<button onclick={() => deleteStack(stack.id)} class="px-3 py-1 rounded text-sm" style="color: var(--color-danger); border: 1px solid var(--color-danger);" disabled={busy === `delete:${stack.id}`}>
							{busy === `delete:${stack.id}` ? 'Removing...' : 'Remove'}
						</button>
					</div>
				</div>

				<!-- Per-service health -->
				{#if loadingServices[stack.id]}
					<div class="text-xs py-2" style="color: var(--color-text-muted);">Loading services...</div>
				{:else if stackServices[stack.id]?.length}
					<div class="services-grid">
						{#each stackServices[stack.id] as svc}
							<div class="svc-card" class:svc-healthy={svc.healthy} class:svc-unhealthy={!svc.healthy}>
								<div class="flex items-center gap-2">
									<span class="svc-dot" style="background-color: {svc.healthy ? 'var(--color-success)' : 'var(--color-danger)'};"></span>
									<span class="font-medium text-sm truncate">{svc.name}</span>
								</div>
								<div class="flex items-center gap-3 mt-1">
									<span class="text-xs font-mono" style="color: {svc.healthy ? 'var(--color-success)' : 'var(--color-danger)'};">
										{svc.running}/{svc.replicas}
									</span>
									<span class="text-xs truncate" style="color: var(--color-text-muted);">{svc.image.split('@')[0]}</span>
								</div>
							</div>
						{/each}
					</div>
				{:else}
					<div class="text-xs py-2" style="color: var(--color-text-muted);">No services found</div>
				{/if}

				{#if editingId === stack.id}
					<div class="space-y-3 mt-4">
						<input bind:value={editDomain} placeholder="Optional domain (example.com)" class="w-full px-3 py-2 rounded-lg text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
						<textarea bind:value={editContent} rows="12" class="w-full px-3 py-2 rounded-lg text-sm font-mono" style="background-color: var(--color-bg); border: 1px solid var(--color-border);"></textarea>
						<button onclick={() => updateStack(stack.id)} class="px-4 py-2 rounded-lg text-sm font-medium" style="background-color: var(--color-primary); color: var(--color-bg);" disabled={busy === `update:${stack.id}`}>
							{busy === `update:${stack.id}` ? 'Updating...' : 'Update & Redeploy'}
						</button>
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

<style>
	.services-grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
		gap: 0.5rem;
	}
	.svc-card {
		padding: 0.5rem 0.75rem;
		border-radius: var(--radius-md);
		border: 1px solid var(--color-border);
		background-color: var(--color-bg);
	}
	.svc-card.svc-unhealthy {
		border-color: rgba(239, 68, 68, 0.3);
	}
	.svc-card.svc-healthy {
		border-color: rgba(34, 197, 94, 0.2);
	}
	.svc-dot {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		flex-shrink: 0;
	}
</style>
