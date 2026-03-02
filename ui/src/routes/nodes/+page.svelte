<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type SwarmNode } from '$lib/api';

	let nodes = $state<SwarmNode[]>([]);
	let joinTokens = $state<{ worker: string; manager: string } | null>(null);
	let error = $state('');
	let showTokens = $state(false);

	onMount(async () => {
		try {
			const data = await api.listNodes();
			nodes = data.nodes ?? [];
			joinTokens = data.join_tokens ?? null;
		} catch (e: any) {
			error = e.message;
		}
	});

	function formatBytes(bytes: number): string {
		if (bytes === 0) return '0 B';
		const k = 1024;
		const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
		const i = Math.floor(Math.log(bytes) / Math.log(k));
		return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
	}

	function formatCPUs(nanoCPUs: number): string {
		return (nanoCPUs / 1e9).toFixed(0) + ' cores';
	}

	function copyToClipboard(text: string) {
		navigator.clipboard.writeText(text);
	}
</script>

<div>
	<div class="flex items-center justify-between mb-6">
		<h2 class="text-2xl font-bold">Nodes</h2>
		{#if joinTokens}
			<button
				onclick={() => showTokens = !showTokens}
				class="px-4 py-2 rounded-lg text-sm font-medium cursor-pointer"
				style="background-color: var(--color-primary); color: var(--color-bg);"
			>
				{showTokens ? 'Hide' : 'Show'} Join Tokens
			</button>
		{/if}
	</div>

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);">
			<p style="color: var(--color-danger);">{error}</p>
		</div>
	{/if}

	{#if showTokens && joinTokens}
		<div class="rounded-lg p-4 mb-6 space-y-3" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="font-semibold text-sm">Add a worker node:</h3>
			<div class="flex items-center gap-2">
				<code class="flex-1 text-xs p-2 rounded overflow-x-auto" style="background-color: var(--color-bg); color: var(--color-text-muted);">
					docker swarm join --token {joinTokens.worker} &lt;MANAGER_IP&gt;:2377
				</code>
				<button onclick={() => copyToClipboard(`docker swarm join --token ${joinTokens!.worker} <MANAGER_IP>:2377`)} class="text-xs px-2 py-1 rounded cursor-pointer" style="background-color: var(--color-bg); color: var(--color-text-muted);">
					Copy
				</button>
			</div>
			<h3 class="font-semibold text-sm mt-4">Add a manager node:</h3>
			<div class="flex items-center gap-2">
				<code class="flex-1 text-xs p-2 rounded overflow-x-auto" style="background-color: var(--color-bg); color: var(--color-text-muted);">
					docker swarm join --token {joinTokens.manager} &lt;MANAGER_IP&gt;:2377
				</code>
				<button onclick={() => copyToClipboard(`docker swarm join --token ${joinTokens!.manager} <MANAGER_IP>:2377`)} class="text-xs px-2 py-1 rounded cursor-pointer" style="background-color: var(--color-bg); color: var(--color-text-muted);">
					Copy
				</button>
			</div>
		</div>
	{/if}

	<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
		{#each nodes as node}
			<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<div class="flex items-center justify-between mb-3">
					<h3 class="font-semibold">{node.Description.Hostname}</h3>
					<span class="text-xs px-2 py-0.5 rounded font-medium"
						style="background-color: {node.Status.State === 'ready' ? 'rgba(34,197,94,0.15)' : 'rgba(239,68,68,0.15)'}; color: {node.Status.State === 'ready' ? 'var(--color-success)' : 'var(--color-danger)'};">
						{node.Status.State}
					</span>
				</div>
				<div class="space-y-2 text-sm">
					<div class="flex justify-between">
						<span style="color: var(--color-text-muted);">Role</span>
						<span class="capitalize">{node.Spec.Role}</span>
					</div>
					<div class="flex justify-between">
						<span style="color: var(--color-text-muted);">Address</span>
						<span>{node.Status.Addr}</span>
					</div>
					<div class="flex justify-between">
						<span style="color: var(--color-text-muted);">CPU</span>
						<span>{formatCPUs(node.Description.Resources.NanoCPUs)}</span>
					</div>
					<div class="flex justify-between">
						<span style="color: var(--color-text-muted);">Memory</span>
						<span>{formatBytes(node.Description.Resources.MemoryBytes)}</span>
					</div>
					<div class="flex justify-between">
						<span style="color: var(--color-text-muted);">OS / Arch</span>
						<span>{node.Description.Platform.OS}/{node.Description.Platform.Architecture}</span>
					</div>
					<div class="flex justify-between">
						<span style="color: var(--color-text-muted);">Availability</span>
						<span class="capitalize">{node.Spec.Availability}</span>
					</div>
				</div>
			</div>
		{/each}
	</div>

	{#if nodes.length === 0 && !error}
		<div class="text-center py-12" style="color: var(--color-text-muted);">
			<p>No nodes found. Is Docker Swarm initialized?</p>
		</div>
	{/if}
</div>
