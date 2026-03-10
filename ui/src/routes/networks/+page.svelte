<script lang="ts">
	import { api } from '$lib/api';
	import { invalidateAll } from '$app/navigation';
	import type { OverlayNetwork } from '$lib/types';
	import { Button, Badge, EmptyState, Alert } from '$lib/components';

	let { data } = $props();

	let networks = $derived(data.networks ?? []);
	let error = $state('');
	let loading = $state<string | null>(null);
	let showForm = $state(false);
	let formName = $state('');
	let formEncrypted = $state(false);
	let formAttachable = $state(false);

	async function createNetwork() {
		if (!formName.trim()) return;
		loading = 'create';
		error = '';
		try {
			await api.createNetwork({
				name: formName.trim(),
				encrypted: formEncrypted,
				attachable: formAttachable
			});
			await invalidateAll();
			showForm = false;
			formName = '';
			formEncrypted = false;
			formAttachable = false;
		} catch (e: unknown) {
			error = e instanceof Error ? e.message : 'Failed to create network';
		} finally {
			loading = null;
		}
	}

	async function removeNetwork(id: string) {
		loading = id;
		error = '';
		try {
			await api.removeNetwork(id);
			await invalidateAll();
		} catch (e: unknown) {
			error = e instanceof Error ? e.message : 'Failed to remove network';
		} finally {
			loading = null;
		}
	}
</script>

<svelte:head><title>Networks | Hive</title></svelte:head>

<div class="max-w-4xl mx-auto">
	<div class="page-header">
		<div>
			<h2 class="page-title">Networks</h2>
			<p class="page-subtitle">
				{networks.length} overlay network{networks.length !== 1 ? 's' : ''} in cluster
			</p>
		</div>
		<Button variant="primary" onclick={() => (showForm = !showForm)}>
			{showForm ? 'Cancel' : 'Create Network'}
		</Button>
	</div>

	{#if error}
		<Alert variant="danger" class="mb-4">
			<p style="color: var(--color-danger);">{error}</p>
		</Alert>
	{/if}

	{#if showForm}
		<div class="hive-card mb-6">
			<div class="hive-card-body space-y-4">
				<h3 class="text-sm font-semibold text-slate-300">Create overlay network</h3>
				<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
					<div>
						<label class="block text-xs font-medium text-slate-400 mb-1">
							Name
							<input
								type="text"
								bind:value={formName}
								placeholder="e.g. backend"
								class="block w-full mt-1 px-3 py-2 rounded-lg bg-slate-900 border border-slate-700 text-slate-200 placeholder-slate-500 focus:border-amber-500/50 focus:ring-1 focus:ring-amber-500/30"
							/>
						</label>
					</div>
					<div class="flex flex-col gap-2 sm:col-span-2">
						<label class="flex items-center gap-2 cursor-pointer" for="form-encrypted">
							<input id="form-encrypted" type="checkbox" bind:checked={formEncrypted} class="rounded border-slate-600 bg-slate-800 text-amber-500 focus:ring-amber-500/50" />
							<span class="text-sm text-slate-400">Encrypted</span>
						</label>
						<label class="flex items-center gap-2 cursor-pointer" for="form-attachable">
							<input id="form-attachable" type="checkbox" bind:checked={formAttachable} class="rounded border-slate-600 bg-slate-800 text-amber-500 focus:ring-amber-500/50" />
							<span class="text-sm text-slate-400">Attachable (standalone containers)</span>
						</label>
					</div>
				</div>
				<div class="flex gap-2">
					<Button variant="primary" onclick={createNetwork} disabled={!formName.trim()} loading={loading === 'create'}>
						Create
					</Button>
					<Button variant="ghost" onclick={() => { showForm = false; formName = ''; }}>Cancel</Button>
				</div>
			</div>
		</div>
	{/if}

	<div class="space-y-3">
		{#each networks as network (network.id)}
			<div class="hive-card flex flex-col sm:flex-row sm:items-center justify-between gap-4">
				<div class="hive-card-body flex-1 min-w-0">
					<div class="flex flex-wrap items-center gap-2 mb-1">
						<h3 class="font-semibold text-slate-200">{network.name}</h3>
						{#if network.encrypted}
							<Badge variant="success">Encrypted</Badge>
						{/if}
						{#if network.attachable}
							<Badge variant="neutral">Attachable</Badge>
						{/if}
					</div>
					<div class="flex flex-wrap gap-3 text-sm text-slate-400">
						<span>Driver: {network.driver}</span>
						<span>Containers: {network.containers ?? 0}</span>
					</div>
				</div>
				<div class="px-4 pb-4 sm:pb-0 sm:px-0 shrink-0">
					<Button
						variant="danger"
						size="sm"
						onclick={() => removeNetwork(network.id)}
						disabled={loading === network.id || (network.containers ?? 0) > 0}
						loading={loading === network.id}
						title={(network.containers ?? 0) > 0 ? 'Remove containers first' : 'Delete network'}
					>
						Delete
					</Button>
				</div>
			</div>
		{/each}
	</div>

	{#if networks.length === 0 && !showForm}
		<EmptyState
			title="No overlay networks"
			description="Create an overlay network to enable multi-node service communication."
		/>
	{/if}
</div>
