<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { api, type CephCluster, type CephHealthReport, type CephOSD, type CephPool } from '$lib/api';

	let cluster = $state<CephCluster | null>(null);
	let health = $state<CephHealthReport | null>(null);
	let osds = $state<CephOSD[]>([]);
	let pools = $state<CephPool[]>([]);
	let error = $state('');
	let loading = $state(true);
	let destroying = $state(false);
	let showDestroy = $state(false);

	$effect(() => {
		loadData();
	});

	async function loadData() {
		const clusterId = $page.params.clusterId;
		try {
			const data = await api.getCephCluster(clusterId);
			cluster = data.cluster;
			health = data.health;
			osds = await api.listCephOSDs(clusterId);
			pools = await api.listCephPools(clusterId);
		} catch (e: any) {
			error = e.message;
		} finally {
			loading = false;
		}
	}

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
			case 'active': return 'color: #22c55e';
			case 'failed': return 'color: #ef4444';
			case 'provisioning': return 'color: #3b82f6';
			case 'bootstrapping':
			case 'expanding': return 'color: #3b82f6';
			default: return 'color: var(--color-muted)';
		}
	}

	function healthBg(h: string): string {
		switch (h) {
			case 'HEALTH_OK': return 'background-color: rgba(34, 197, 94, 0.1); border-color: #22c55e;';
			case 'HEALTH_WARN': return 'background-color: rgba(234, 179, 8, 0.1); border-color: #eab308;';
			case 'HEALTH_ERR': return 'background-color: rgba(239, 68, 68, 0.1); border-color: #ef4444;';
			default: return 'background-color: var(--color-surface);';
		}
	}

	function usagePercent(): number {
		if (!health || !health.total_bytes) return 0;
		return Math.round((health.used_bytes / health.total_bytes) * 100);
	}

	async function destroyCluster() {
		if (!cluster) return;
		destroying = true;
		try {
			await api.deleteCephCluster(cluster.id);
			goto('/storage/ceph');
		} catch (e: any) {
			error = e.message;
			destroying = false;
		}
	}
</script>

<div>
	<div class="flex items-center gap-4 mb-6">
		<a href="/storage/ceph" style="color: var(--color-muted); text-decoration: none;">← Ceph Clusters</a>
		{#if cluster}
			<h2 class="text-2xl font-bold">{cluster.name}</h2>
			<span class="text-sm font-medium uppercase" style={statusColor(cluster.status)}>{cluster.status}</span>
		{/if}
	</div>

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);">
			<p style="color: var(--color-danger);">{error}</p>
		</div>
	{/if}

	{#if loading}
		<p style="color: var(--color-muted);">Loading cluster details...</p>
	{:else if cluster}
		<!-- Health Banner -->
		{#if health}
			<div class="rounded-lg p-4 mb-6" style={healthBg(health.health) + ' border: 1px solid;'}>
				<div class="flex items-center justify-between mb-2">
					<h3 class="font-semibold">{health.health.replace('HEALTH_', '')}</h3>
					<span class="text-sm" style="color: var(--color-muted);">
						Last updated: {new Date(health.timestamp * 1000).toLocaleTimeString()}
					</span>
				</div>
				{#if health.health_detail && health.health_detail.length > 0}
					<ul class="text-sm space-y-1">
						{#each health.health_detail as detail}
							<li>- {detail}</li>
						{/each}
					</ul>
				{/if}
			</div>
		{/if}

		<!-- Overview Cards -->
		<div class="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
			<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<p class="text-sm" style="color: var(--color-muted);">Monitors</p>
				<p class="text-2xl font-bold">
					{health ? health.mon_count : cluster.mon_hosts?.length || 0}
				</p>
				{#if health}
					<p class="text-xs" style="color: var(--color-muted);">{health.mon_quorum?.length || 0} in quorum</p>
				{/if}
			</div>
			<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<p class="text-sm" style="color: var(--color-muted);">OSDs</p>
				<p class="text-2xl font-bold">
					{health ? `${health.osd_up}/${health.osd_total}` : osds.length}
				</p>
				<p class="text-xs" style="color: var(--color-muted);">up / total</p>
			</div>
			<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<p class="text-sm" style="color: var(--color-muted);">Storage Used</p>
				<p class="text-2xl font-bold">
					{health ? formatBytes(health.used_bytes) : '---'}
				</p>
				{#if health}
					<p class="text-xs" style="color: var(--color-muted);">of {formatBytes(health.total_bytes)} ({usagePercent()}%)</p>
				{/if}
			</div>
			<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<p class="text-sm" style="color: var(--color-muted);">Placement Groups</p>
				<p class="text-2xl font-bold">{health?.pg_count || 0}</p>
				<p class="text-xs" style="color: var(--color-muted);">replication: {cluster.replication_size}x</p>
			</div>
		</div>

		{#if health && health.total_bytes}
			<div class="rounded-lg p-4 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<p class="text-sm font-medium mb-2">Storage Usage</p>
				<div class="w-full rounded-full h-4" style="background-color: var(--color-bg);">
					<div
						class="h-4 rounded-full"
						style="width: {usagePercent()}%; background-color: {usagePercent() > 85 ? '#ef4444' : usagePercent() > 70 ? '#eab308' : '#22c55e'};"
					></div>
				</div>
				<div class="flex justify-between text-xs mt-1" style="color: var(--color-muted);">
					<span>{formatBytes(health.used_bytes)} used</span>
					<span>{formatBytes(health.avail_bytes)} available</span>
				</div>
			</div>
		{/if}

		<!-- Cluster Info -->
		<div class="rounded-lg p-4 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="font-semibold mb-3">Cluster Info</h3>
			<div class="grid grid-cols-2 gap-3 text-sm">
				<div>
					<span style="color: var(--color-muted);">FSID</span>
					<p class="font-mono">{cluster.fsid || 'pending'}</p>
				</div>
				<div>
					<span style="color: var(--color-muted);">Monitor Hosts</span>
					<p class="font-mono">{cluster.mon_hosts?.join(', ') || 'none'}</p>
				</div>
				<div>
					<span style="color: var(--color-muted);">Public Network</span>
					<p>{cluster.public_network || 'auto'}</p>
				</div>
				<div>
					<span style="color: var(--color-muted);">Created</span>
					<p>{new Date(cluster.created_at).toLocaleString()}</p>
				</div>
			</div>
		</div>

		<!-- OSDs Table -->
		<div class="rounded-lg p-4 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="font-semibold mb-3">OSDs ({osds.length})</h3>
			{#if osds.length === 0}
				<p class="text-sm" style="color: var(--color-muted);">No OSDs registered.</p>
			{:else}
				<div class="overflow-x-auto">
					<table class="w-full text-sm">
						<thead>
							<tr style="border-bottom: 1px solid var(--color-border);">
								<th class="text-left py-2 pr-4">OSD ID</th>
								<th class="text-left py-2 pr-4">Node</th>
								<th class="text-left py-2 pr-4">Device</th>
								<th class="text-left py-2 pr-4">Size</th>
								<th class="text-left py-2 pr-4">Type</th>
								<th class="text-left py-2">Status</th>
							</tr>
						</thead>
						<tbody>
							{#each osds as osd}
								<tr style="border-bottom: 1px solid var(--color-border);">
									<td class="py-2 pr-4 font-mono">{osd.osd_id ?? '-'}</td>
									<td class="py-2 pr-4">{osd.hostname}</td>
									<td class="py-2 pr-4 font-mono">{osd.device_path}</td>
									<td class="py-2 pr-4">{formatBytes(osd.device_size)}</td>
									<td class="py-2 pr-4 uppercase text-xs">{osd.device_type}</td>
									<td class="py-2">
										<span class="text-xs font-medium uppercase" style={statusColor(osd.status)}>{osd.status}</span>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</div>

		<!-- Pools Table -->
		<div class="rounded-lg p-4 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="font-semibold mb-3">Pools ({pools.length})</h3>
			{#if pools.length === 0}
				<p class="text-sm" style="color: var(--color-muted);">No pools created.</p>
			{:else}
				<div class="overflow-x-auto">
					<table class="w-full text-sm">
						<thead>
							<tr style="border-bottom: 1px solid var(--color-border);">
								<th class="text-left py-2 pr-4">Name</th>
								<th class="text-left py-2 pr-4">Pool ID</th>
								<th class="text-left py-2 pr-4">PGs</th>
								<th class="text-left py-2 pr-4">Size</th>
								<th class="text-left py-2 pr-4">Type</th>
								<th class="text-left py-2">Application</th>
							</tr>
						</thead>
						<tbody>
							{#each pools as pool}
								<tr style="border-bottom: 1px solid var(--color-border);">
									<td class="py-2 pr-4 font-medium">{pool.name}</td>
									<td class="py-2 pr-4">{pool.pool_id ?? '-'}</td>
									<td class="py-2 pr-4">{pool.pg_num}</td>
									<td class="py-2 pr-4">{pool.size}x</td>
									<td class="py-2 pr-4">{pool.type}</td>
									<td class="py-2">{pool.application}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}

			{#if health && health.pools && health.pools.length > 0}
				<h4 class="font-medium mt-4 mb-2 text-sm">Pool Usage (Live)</h4>
				<div class="space-y-2">
					{#each health.pools as poolStat}
						<div class="flex items-center justify-between text-sm">
							<span class="font-medium">{poolStat.name}</span>
							<span>{formatBytes(poolStat.used_bytes)} used, {formatBytes(poolStat.max_avail)} available, {poolStat.objects} objects</span>
						</div>
					{/each}
				</div>
			{/if}
		</div>

		<!-- Danger Zone -->
		<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-danger);">
			<h3 class="font-semibold mb-2" style="color: var(--color-danger);">Danger Zone</h3>
			{#if !showDestroy}
				<button
					onclick={() => showDestroy = true}
					class="px-4 py-2 rounded-lg text-sm font-medium cursor-pointer"
					style="background-color: transparent; border: 1px solid var(--color-danger); color: var(--color-danger);"
				>
					Destroy Cluster
				</button>
			{:else}
				<p class="text-sm mb-3" style="color: var(--color-danger);">
					This will remove all Ceph daemons, destroy all data on OSDs, and remove the associated storage host. This action cannot be undone.
				</p>
				<div class="flex gap-3">
					<button
						onclick={() => showDestroy = false}
						class="px-4 py-2 rounded-lg text-sm font-medium cursor-pointer"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
					>
						Cancel
					</button>
					<button
						onclick={destroyCluster}
						disabled={destroying}
						class="px-4 py-2 rounded-lg text-sm font-medium cursor-pointer"
						style="background-color: var(--color-danger); color: white;"
					>
						{destroying ? 'Destroying...' : 'Yes, Destroy Cluster'}
					</button>
				</div>
			{/if}
		</div>
	{/if}
</div>
