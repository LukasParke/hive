<script lang="ts">
	import { api } from '$lib/api';
	import type { SwarmNode, PrometheusNodeCurrent } from '$lib/types';
	import { Button, EmptyState, Badge, Alert, NodeCard } from '$lib/components';
	import { metricsStore } from '$lib/stores/metrics.svelte';
	import { onMount } from 'svelte';

	let { data } = $props();

	let error = $state('');
	let showTokens = $state(false);
	let actionLoading = $state<string | null>(null);

	const ms = $derived(metricsStore.state);
	const promNodes = $derived(ms.nodes);

	onMount(() => {
		return metricsStore.subscribe();
	});

	function getPromNode(hostname: string): PrometheusNodeCurrent | null {
		return promNodes.find((p) => p.hostname === hostname) ?? null;
	}

	function copyToClipboard(text: string) {
		navigator.clipboard.writeText(text);
	}

	async function setAvailability(nodeId: string, availability: string) {
		actionLoading = nodeId;
		error = '';
		try {
			await api.updateNodeAvailability(nodeId, availability);
			const updated = await api.listNodes();
			data.nodes = (updated.nodes ?? []) as SwarmNode[];
		} catch (e: any) {
			error = e.message;
		} finally {
			actionLoading = null;
		}
	}

	function statusLabel(node: SwarmNode): string {
		if (node.Status.State !== 'ready') return 'Down';
		if (node.Spec.Availability === 'drain') return 'Draining';
		if (node.Spec.Availability === 'pause') return 'Paused';
		return 'Ready';
	}

	function statusVariant(node: SwarmNode): 'success' | 'danger' | 'warning' {
		if (node.Status.State !== 'ready') return 'danger';
		if (node.Spec.Availability === 'drain' || node.Spec.Availability === 'pause') return 'warning';
		return 'success';
	}
</script>

<svelte:head><title>Nodes | Hive</title></svelte:head>

<div class="max-w-7xl mx-auto">
	<div class="page-header">
		<div>
			<h2 class="page-title">Nodes</h2>
			<p class="page-subtitle">{(data.nodes ?? []).length} node{(data.nodes ?? []).length !== 1 ? 's' : ''} in cluster</p>
		</div>
		{#if data.joinTokens}
			<Button variant="primary" onclick={() => showTokens = !showTokens}>
				{showTokens ? 'Hide' : 'Show'} Join Tokens
			</Button>
		{/if}
	</div>

	{#if error}
		<Alert variant="danger" class="mb-4">
			<p style="color: var(--color-danger);">{error}</p>
		</Alert>
	{/if}

	{#if showTokens && data.joinTokens}
		{@const addr = (data as any).advertiseAddr || '<MANAGER_IP>:2377'}
		<div class="hive-card mb-6">
			<div class="hive-card-body space-y-3">
				<h3 class="font-semibold text-sm">Add a worker node:</h3>
				<div class="flex flex-col sm:flex-row sm:items-center gap-2">
					<code class="flex-1 text-xs p-2 rounded overflow-x-auto" style="background-color: var(--color-bg); color: var(--color-text-muted);">
						docker swarm join --token {data.joinTokens.worker} {addr}
					</code>
					<Button size="sm" onclick={() => copyToClipboard(`docker swarm join --token ${data.joinTokens!.worker} ${addr}`)}>
						Copy
					</Button>
				</div>
				<h3 class="font-semibold text-sm mt-4">Add a manager node:</h3>
				<div class="flex flex-col sm:flex-row sm:items-center gap-2">
					<code class="flex-1 text-xs p-2 rounded overflow-x-auto" style="background-color: var(--color-bg); color: var(--color-text-muted);">
						docker swarm join --token {data.joinTokens.manager} {addr}
					</code>
					<Button size="sm" onclick={() => copyToClipboard(`docker swarm join --token ${data.joinTokens!.manager} ${addr}`)}>
						Copy
					</Button>
				</div>
			</div>
		</div>
	{/if}

	<div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
		{#each data.nodes ?? [] as node}
			{@const prom = getPromNode(node.Description.Hostname)}
			<div class="node-wrapper">
				<NodeCard
					hostname={node.Description.Hostname}
					nodeId={node.Description.Hostname}
					role={node.Spec.Role}
					state={node.Status.State === 'ready' ? (node.Spec.Availability === 'active' ? 'ready' : node.Spec.Availability) : 'down'}
					addr={node.Status.Addr}
					cores={Math.round(node.Description.Resources.NanoCPUs / 1e9)}
					memTotal={node.Description.Resources.MemoryBytes}
					prom={prom}
				/>
				<!-- Quick actions bar -->
				<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
				<div class="node-actions" onclick={(e) => e.stopPropagation()}>
					<Badge variant={statusVariant(node)} dot>{statusLabel(node)}</Badge>
					<div class="flex gap-2">
						{#if node.Spec.Availability === 'active'}
							<Button size="sm" variant="secondary"
								onclick={() => setAvailability(node.ID, 'drain')}
								disabled={actionLoading === node.ID}
								loading={actionLoading === node.ID}>
								Drain
							</Button>
							<Button size="sm" variant="ghost"
								onclick={() => setAvailability(node.ID, 'pause')}
								disabled={actionLoading === node.ID}>
								Pause
							</Button>
						{:else}
							<Button size="sm" variant="primary"
								onclick={() => setAvailability(node.ID, 'active')}
								disabled={actionLoading === node.ID}
								loading={actionLoading === node.ID}>
								Activate
							</Button>
						{/if}
					</div>
				</div>
			</div>
		{/each}
	</div>

	{#if (data.nodes ?? []).length === 0 && !error}
		<EmptyState title="No nodes found" description="Is Docker Swarm initialized?" />
	{/if}
</div>

<style>
	.node-wrapper {
		display: flex;
		flex-direction: column;
	}
	.node-wrapper > :global(.node-card) {
		border-bottom-left-radius: 0;
		border-bottom-right-radius: 0;
	}
	.node-actions {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0.5rem 1.25rem;
		background-color: var(--color-surface);
		border: 1px solid var(--color-border);
		border-top: none;
		border-bottom-left-radius: var(--radius-lg);
		border-bottom-right-radius: var(--radius-lg);
	}
	@media (max-width: 768px) {
		.node-actions {
			padding: 0.5rem 1rem;
		}
	}
</style>
