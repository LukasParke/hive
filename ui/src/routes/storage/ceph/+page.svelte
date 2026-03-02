<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type CephClusterWithHealth } from '$lib/api';

	let clusters = $state<CephClusterWithHealth[]>([]);
	let error = $state('');
	let loading = $state(true);

	onMount(async () => {
		try {
			clusters = await api.listCephClusters();
		} catch (e: any) {
			error = e.message;
		} finally {
			loading = false;
		}
	});

	function formatBytes(bytes: number): string {
		if (!bytes || bytes === 0) return '0 B';
		const k = 1024;
		const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
		const i = Math.floor(Math.log(bytes) / Math.log(k));
		return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
	}

	function statusColor(status: string): string {
		switch (status) {
			case 'healthy': return 'color: #22c55e';
			case 'degraded': return 'color: #eab308';
			case 'error': return 'color: #ef4444';
			case 'bootstrapping':
			case 'expanding': return 'color: #3b82f6';
			default: return 'color: var(--color-muted)';
		}
	}

	function healthColor(health: string): string {
		switch (health) {
			case 'HEALTH_OK': return 'color: #22c55e';
			case 'HEALTH_WARN': return 'color: #eab308';
			case 'HEALTH_ERR': return 'color: #ef4444';
			default: return 'color: var(--color-muted)';
		}
	}
</script>

<div>
	<div class="flex items-center justify-between mb-6">
		<h2 class="text-2xl font-bold">Ceph Storage</h2>
		<a
			href="/storage/ceph/deploy"
			class="px-4 py-2 rounded-lg text-sm font-medium"
			style="background-color: var(--color-primary); color: var(--color-bg); text-decoration: none;"
		>
			Deploy New Cluster
		</a>
	</div>

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);">
			<p style="color: var(--color-danger);">{error}</p>
		</div>
	{/if}

	{#if loading}
		<p style="color: var(--color-muted);">Loading clusters...</p>
	{:else if clusters.length === 0}
		<div class="rounded-lg p-8 text-center" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<p class="text-lg font-medium mb-2">No Ceph Clusters</p>
			<p style="color: var(--color-muted);">Deploy a Ceph cluster to get started with distributed HA storage.</p>
		</div>
	{:else}
		<div class="grid gap-4">
			{#each clusters as cluster}
				<a
					href="/storage/ceph/{cluster.id}"
					class="block rounded-lg p-5"
					style="background-color: var(--color-surface); border: 1px solid var(--color-border); text-decoration: none; color: inherit;"
				>
					<div class="flex items-center justify-between mb-3">
						<h3 class="text-lg font-semibold">{cluster.name}</h3>
						<span class="text-sm font-medium uppercase" style={statusColor(cluster.status)}>
							{cluster.status}
						</span>
					</div>

					<div class="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
						<div>
							<span style="color: var(--color-muted);">FSID</span>
							<p class="font-mono text-xs mt-1">{cluster.fsid || 'pending'}</p>
						</div>
						<div>
							<span style="color: var(--color-muted);">Monitors</span>
							<p class="mt-1">{cluster.mon_hosts?.length || 0} nodes</p>
						</div>
						<div>
							<span style="color: var(--color-muted);">Replication</span>
							<p class="mt-1">{cluster.replication_size}x</p>
						</div>
						{#if cluster.health}
							<div>
								<span style="color: var(--color-muted);">Health</span>
								<p class="mt-1" style={healthColor(cluster.health.health)}>
									{cluster.health.health.replace('HEALTH_', '')}
								</p>
							</div>
						{/if}
					</div>

					{#if cluster.health}
						<div class="mt-3 pt-3" style="border-top: 1px solid var(--color-border);">
							<div class="flex gap-6 text-sm">
								<span>OSDs: {cluster.health.osd_up}/{cluster.health.osd_total} up</span>
								<span>Storage: {formatBytes(cluster.health.used_bytes)} / {formatBytes(cluster.health.total_bytes)}</span>
								<span>PGs: {cluster.health.pg_count}</span>
							</div>
						</div>
					{/if}
				</a>
			{/each}
		</div>
	{/if}
</div>
