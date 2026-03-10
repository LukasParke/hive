<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '$lib/api';

	let { data } = $props();

	let error = $state('');
	let destroying = $state(false);
	let showDestroy = $state(false);

	function formatBytes(bytes: number): string {
		if (!bytes || bytes === 0) return '0 B';
		const k = 1024;
		const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
		const i = Math.floor(Math.log(bytes) / Math.log(k));
		return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
	}

	function statusColor(status: string): string {
		switch (status) {
			case 'healthy': return 'color: var(--color-success)';
			case 'degraded': return 'color: var(--color-primary)';
			case 'error': return 'color: var(--color-danger)';
			case 'active': return 'color: var(--color-success)';
			case 'failed': return 'color: var(--color-danger)';
			case 'provisioning': return 'color: var(--color-info)';
			case 'bootstrapping':
			case 'expanding': return 'color: var(--color-info)';
			default: return 'color: var(--color-muted)';
		}
	}

	function healthBg(h: string): string {
		switch (h) {
			case 'HEALTH_OK': return 'background-color: rgba(34, 197, 94, 0.1); border-color: var(--color-success);';
			case 'HEALTH_WARN': return 'background-color: rgba(229, 160, 13, 0.1); border-color: var(--color-primary);';
			case 'HEALTH_ERR': return 'background-color: rgba(239, 68, 68, 0.1); border-color: var(--color-danger);';
			default: return 'background-color: var(--color-surface);';
		}
	}

	function usagePercent(): number {
		if (!data.health || !data.health.total_bytes) return 0;
		return Math.round((data.health.used_bytes / data.health.total_bytes) * 100);
	}

	async function destroyCluster() {
		if (!data.cluster) return;
		destroying = true;
		try {
			await api.deleteCephCluster(data.cluster.id);
			goto('/ceph');
		} catch (e: any) {
			error = e.message;
			destroying = false;
		}
	}
</script>

<svelte:head><title>Ceph Cluster | Hive</title></svelte:head>

<div>
	<div class="flex items-center gap-4 mb-6">
		<a href="/ceph" style="color: var(--color-muted); text-decoration: none;">← Ceph Clusters</a>
		{#if data.cluster}
			<h2 class="text-2xl font-bold">{data.cluster.name}</h2>
			<span class="text-sm font-medium uppercase" style={statusColor(data.cluster.status)}>{data.cluster.status}</span>
		{/if}
	</div>

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);">
			<p style="color: var(--color-danger);">{error}</p>
		</div>
	{/if}

	{#if data.cluster}
		<!-- Health Banner -->
		{#if data.health}
			<div class="rounded-lg p-4 mb-6" style={healthBg(data.health.health) + ' border: 1px solid;'}>
				<div class="flex items-center justify-between mb-2">
					<h3 class="font-semibold">{data.health.health.replace('HEALTH_', '')}</h3>
					<span class="text-sm" style="color: var(--color-muted);">
						Last updated: {new Date(data.health.timestamp * 1000).toLocaleTimeString()}
					</span>
				</div>
				{#if (data.health?.health_detail ?? []).length > 0}
					<ul class="text-sm space-y-1">
						{#each (data.health?.health_detail ?? []) as detail}
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
					{data.health ? data.health.mon_count : data.cluster.mon_hosts?.length || 0}
				</p>
				{#if data.health}
					<p class="text-xs" style="color: var(--color-muted);">{data.health.mon_quorum?.length || 0} in quorum</p>
				{/if}
			</div>
			<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<p class="text-sm" style="color: var(--color-muted);">OSDs</p>
				<p class="text-2xl font-bold">
					{data.health ? `${data.health.osd_up}/${data.health.osd_total}` : (data.osds ?? []).length}
				</p>
				<p class="text-xs" style="color: var(--color-muted);">up / total</p>
			</div>
			<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<p class="text-sm" style="color: var(--color-muted);">Storage Used</p>
				<p class="text-2xl font-bold">
					{data.health ? formatBytes(data.health.used_bytes) : '---'}
				</p>
				{#if data.health}
					<p class="text-xs" style="color: var(--color-muted);">of {formatBytes(data.health.total_bytes)} ({usagePercent()}%)</p>
				{/if}
			</div>
			<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<p class="text-sm" style="color: var(--color-muted);">Placement Groups</p>
				<p class="text-2xl font-bold">{data.health?.pg_count || 0}</p>
				<p class="text-xs" style="color: var(--color-muted);">replication: {data.cluster.replication_size}x</p>
			</div>
		</div>

		{#if data.health && data.health.total_bytes}
			<div class="rounded-lg p-4 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<p class="text-sm font-medium mb-2">Storage Usage</p>
				<div class="w-full rounded-full h-4" style="background-color: var(--color-bg);">
					<div
						class="h-4 rounded-full"
						style="width: {usagePercent()}%; background-color: {usagePercent() > 85 ? 'var(--color-danger)' : usagePercent() > 70 ? 'var(--color-primary)' : 'var(--color-success)'};"
					></div>
				</div>
				<div class="flex justify-between text-xs mt-1" style="color: var(--color-muted);">
					<span>{formatBytes(data.health.used_bytes)} used</span>
					<span>{formatBytes(data.health.avail_bytes)} available</span>
				</div>
			</div>
		{/if}

		<!-- Cluster Info -->
		<div class="rounded-lg p-4 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="font-semibold mb-3">Cluster Info</h3>
			<div class="grid grid-cols-2 gap-3 text-sm">
				<div>
					<span style="color: var(--color-muted);">FSID</span>
					<p class="font-mono">{data.cluster.fsid || 'pending'}</p>
				</div>
				<div>
					<span style="color: var(--color-muted);">Monitor Hosts</span>
					<p class="font-mono">{data.cluster.mon_hosts?.join(', ') || 'none'}</p>
				</div>
				<div>
					<span style="color: var(--color-muted);">Public Network</span>
					<p>{data.cluster.public_network || 'auto'}</p>
				</div>
				<div>
					<span style="color: var(--color-muted);">Created</span>
					<p>{new Date(data.cluster.created_at).toLocaleString()}</p>
				</div>
			</div>
		</div>

		<!-- OSDs Table -->
		<div class="rounded-lg p-4 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="font-semibold mb-3">OSDs ({(data.osds ?? []).length})</h3>
			{#if (data.osds ?? []).length === 0}
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
							{#each data.osds ?? [] as osd}
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
			<h3 class="font-semibold mb-3">Pools ({(data.pools ?? []).length})</h3>
			{#if (data.pools ?? []).length === 0}
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
							{#each data.pools ?? [] as pool}
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

			{#if (data.health?.pools ?? []).length > 0}
				<h4 class="font-medium mt-4 mb-2 text-sm">Pool Usage (Live)</h4>
				<div class="space-y-2">
					{#each (data.health?.pools ?? []) as poolStat}
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
