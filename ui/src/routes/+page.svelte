<script lang="ts">
	import { api, type SystemStatus, type SwarmNode, type ServiceHealth, type NodeMetricsReport } from '$lib/api';
	import { onMount, onDestroy } from 'svelte';

	let status = $state<SystemStatus | null>(null);
	let nodes = $state<SwarmNode[]>([]);
	let serviceHealth = $state<ServiceHealth[]>([]);
	let clusterMetrics = $state<NodeMetricsReport[]>([]);
	let error = $state('');
	let refreshInterval: ReturnType<typeof setInterval>;

	onMount(async () => {
		await loadAll();
		refreshInterval = setInterval(loadMetrics, 10000);
	});

	onDestroy(() => {
		clearInterval(refreshInterval);
	});

	async function loadAll() {
		try {
			const [s, n] = await Promise.all([api.status(), api.listNodes()]);
			status = s;
			nodes = n.nodes ?? [];
			await loadMetrics();
		} catch (e: any) {
			error = e.message;
		}
	}

	async function loadMetrics() {
		try {
			const [health, metrics] = await Promise.all([
				api.metricsServices().catch(() => []),
				api.getClusterMetrics().catch(() => []),
			]);
			serviceHealth = health;
			clusterMetrics = metrics;
		} catch {}
	}

	function formatBytes(bytes: number): string {
		if (!bytes) return '0 B';
		const k = 1024;
		const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
		const i = Math.floor(Math.log(bytes) / Math.log(k));
		return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
	}

	function pct(used: number, total: number): number {
		if (!total) return 0;
		return Math.round((used / total) * 100);
	}

	function barColor(pct: number): string {
		if (pct > 85) return '#ef4444';
		if (pct > 60) return '#f59e0b';
		return '#22c55e';
	}

	function formatUptime(seconds: number): string {
		const days = Math.floor(seconds / 86400);
		const hours = Math.floor((seconds % 86400) / 3600);
		if (days > 0) return `${days}d ${hours}h`;
		const mins = Math.floor((seconds % 3600) / 60);
		return `${hours}h ${mins}m`;
	}

	function isStale(ts: number): boolean {
		return (Date.now() / 1000 - ts) > 60;
	}

	let clusterTotalCores = $derived(clusterMetrics.reduce((s, m) => s + m.cpu_cores, 0));
	let clusterTotalRAM = $derived(clusterMetrics.reduce((s, m) => s + m.mem_total, 0));
	let clusterUsedRAM = $derived(clusterMetrics.reduce((s, m) => s + m.mem_used, 0));
	let clusterTotalDisk = $derived(clusterMetrics.reduce((s, m) => s + m.disks.reduce((d, dk) => d + dk.total, 0), 0));
	let clusterUsedDisk = $derived(clusterMetrics.reduce((s, m) => s + m.disks.reduce((d, dk) => d + dk.used, 0), 0));
	let clusterContainers = $derived(clusterMetrics.reduce((s, m) => s + m.containers_running, 0));
	let clusterAvgCPU = $derived(clusterMetrics.length > 0 ? clusterMetrics.reduce((s, m) => s + m.cpu_total_pct, 0) / clusterMetrics.length : 0);

	let alerts = $derived(() => {
		const a: string[] = [];
		for (const m of clusterMetrics) {
			if (m.cpu_total_pct > 90) a.push(`${m.hostname}: CPU at ${m.cpu_total_pct.toFixed(0)}%`);
			for (const d of m.disks) {
				const dPct = pct(d.used, d.total);
				if (dPct > 85) a.push(`${m.hostname}: Disk ${d.mount_point} at ${dPct}%`);
			}
			if (isStale(m.timestamp)) a.push(`${m.hostname}: No recent metrics (node may be offline)`);
		}
		for (const svc of serviceHealth) {
			if (!svc.healthy) a.push(`Service ${svc.service_name.replace('hive-app-', '')}: ${svc.running}/${svc.replicas} replicas`);
		}
		return a;
	});
</script>

<div class="max-w-7xl mx-auto">
	<h2 class="text-2xl font-bold mb-6">Cluster Dashboard</h2>

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid #ef4444;">
			<p style="color: #ef4444;">{error}</p>
		</div>
	{/if}

	<!-- Alerts banner -->
	{#if alerts().length > 0}
		<div class="rounded-lg p-4 mb-6" style="background-color: rgba(239, 68, 68, 0.08); border: 1px solid rgba(239, 68, 68, 0.3);">
			<h3 class="font-semibold text-sm mb-2" style="color: #ef4444;">Active Alerts ({alerts().length})</h3>
			<ul class="space-y-1 text-sm">
				{#each alerts() as alert}
					<li style="color: #fca5a5;">{alert}</li>
				{/each}
			</ul>
		</div>
	{/if}

	<!-- Cluster summary bar -->
	{#if status}
		<div class="grid grid-cols-2 md:grid-cols-6 gap-3 mb-8">
			<div class="rounded-lg p-4 text-center" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">Nodes</p>
				<p class="text-xl font-bold">{clusterMetrics.length || status.node_count}</p>
			</div>
			<div class="rounded-lg p-4 text-center" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">Total Cores</p>
				<p class="text-xl font-bold">{clusterTotalCores}</p>
			</div>
			<div class="rounded-lg p-4 text-center" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">Total RAM</p>
				<p class="text-xl font-bold">{formatBytes(clusterTotalRAM)}</p>
			</div>
			<div class="rounded-lg p-4 text-center" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">Total Disk</p>
				<p class="text-xl font-bold">{formatBytes(clusterTotalDisk)}</p>
			</div>
			<div class="rounded-lg p-4 text-center" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">Containers</p>
				<p class="text-xl font-bold">{clusterContainers}</p>
			</div>
			<div class="rounded-lg p-4 text-center" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">Avg CPU</p>
				<p class="text-xl font-bold" style="color: {barColor(clusterAvgCPU)};">{clusterAvgCPU.toFixed(0)}%</p>
			</div>
		</div>
	{/if}

	<!-- Per-node cards -->
	{#if clusterMetrics.length > 0}
		<h3 class="text-lg font-semibold mb-4">Node Overview</h3>
		<div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4 mb-8">
			{#each clusterMetrics as m}
				{@const memPct = pct(m.mem_used, m.mem_total)}
				{@const diskTotal = m.disks.reduce((s, d) => s + d.total, 0)}
				{@const diskUsed = m.disks.reduce((s, d) => s + d.used, 0)}
				{@const diskPct = pct(diskUsed, diskTotal)}
				{@const stale = isStale(m.timestamp)}
				<a href="/nodes/{m.node_id}" class="rounded-lg p-5 block hover:scale-[1.01] transition-transform"
					style="background-color: var(--color-surface); border: 1px solid var(--color-border); {stale ? 'opacity: 0.6;' : ''}">
					<div class="flex items-center justify-between mb-3">
						<div class="flex items-center gap-2">
							<span class="w-2.5 h-2.5 rounded-full" style="background-color: {stale ? '#ef4444' : '#22c55e'};"></span>
							<h4 class="font-semibold">{m.hostname}</h4>
						</div>
						<span class="text-xs px-2 py-0.5 rounded" style="background-color: var(--color-bg); color: var(--color-text-muted);">
							{m.cpu_cores} cores
						</span>
					</div>

					<div class="space-y-3">
						<!-- CPU gauge -->
						<div>
							<div class="flex justify-between text-xs mb-1" style="color: var(--color-text-muted);">
								<span>CPU</span>
								<span style="color: {barColor(m.cpu_total_pct)};">{m.cpu_total_pct.toFixed(1)}%</span>
							</div>
							<div class="w-full rounded-full h-2" style="background-color: var(--color-border);">
								<div class="h-2 rounded-full transition-all" style="width: {Math.min(m.cpu_total_pct, 100)}%; background-color: {barColor(m.cpu_total_pct)};"></div>
							</div>
						</div>

						<!-- RAM gauge -->
						<div>
							<div class="flex justify-between text-xs mb-1" style="color: var(--color-text-muted);">
								<span>RAM</span>
								<span>{formatBytes(m.mem_used)} / {formatBytes(m.mem_total)}</span>
							</div>
							<div class="w-full rounded-full h-2" style="background-color: var(--color-border);">
								<div class="h-2 rounded-full transition-all" style="width: {memPct}%; background-color: {barColor(memPct)};"></div>
							</div>
						</div>

						<!-- Disk gauge -->
						<div>
							<div class="flex justify-between text-xs mb-1" style="color: var(--color-text-muted);">
								<span>Disk</span>
								<span>{formatBytes(diskUsed)} / {formatBytes(diskTotal)}</span>
							</div>
							<div class="w-full rounded-full h-2" style="background-color: var(--color-border);">
								<div class="h-2 rounded-full transition-all" style="width: {diskPct}%; background-color: {barColor(diskPct)};"></div>
							</div>
						</div>

						<!-- Bottom stats -->
						<div class="flex justify-between text-xs pt-1" style="color: var(--color-text-muted);">
							<span>{m.containers_running} containers</span>
							{#if m.cpu_temp_celsius > 0}
								<span>{m.cpu_temp_celsius.toFixed(0)}°C</span>
							{/if}
							<span>up {formatUptime(m.uptime_seconds)}</span>
						</div>
					</div>
				</a>
			{/each}
		</div>
	{:else if nodes.length > 0}
		<!-- Fallback: basic nodes table if no agent metrics yet -->
		<h3 class="text-lg font-semibold mb-4">Nodes</h3>
		<div class="rounded-lg overflow-hidden" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<table class="w-full text-sm">
				<thead>
					<tr style="border-bottom: 1px solid var(--color-border);">
						<th class="text-left p-3 font-medium" style="color: var(--color-text-muted);">Hostname</th>
						<th class="text-left p-3 font-medium" style="color: var(--color-text-muted);">Role</th>
						<th class="text-left p-3 font-medium" style="color: var(--color-text-muted);">Status</th>
						<th class="text-left p-3 font-medium" style="color: var(--color-text-muted);">Address</th>
						<th class="text-left p-3 font-medium" style="color: var(--color-text-muted);">Memory</th>
					</tr>
				</thead>
				<tbody>
					{#each nodes as node}
						<tr style="border-bottom: 1px solid var(--color-border);">
							<td class="p-3 font-medium">
								<a href="/nodes/{node.ID}" class="hover:underline">{node.Description.Hostname}</a>
							</td>
							<td class="p-3 capitalize">{node.Spec.Role}</td>
							<td class="p-3">
								<span class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium"
									style="background-color: {node.Status.State === 'ready' ? 'rgba(34,197,94,0.15)' : 'rgba(239,68,68,0.15)'}; color: {node.Status.State === 'ready' ? '#22c55e' : '#ef4444'};">
									{node.Status.State}
								</span>
							</td>
							<td class="p-3" style="color: var(--color-text-muted);">{node.Status.Addr}</td>
							<td class="p-3" style="color: var(--color-text-muted);">{formatBytes(node.Description.Resources.MemoryBytes)}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}

	<!-- Service health grid -->
	{#if serviceHealth.length > 0}
		<h3 class="text-lg font-semibold mb-4">Service Health</h3>
		<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3 mb-8">
			{#each serviceHealth as svc}
				<div class="rounded-lg p-3 flex items-center justify-between" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
					<div class="flex items-center gap-2">
						<span class="inline-block w-2.5 h-2.5 rounded-full" style="background-color: {svc.healthy ? '#22c55e' : '#ef4444'};"></span>
						<span class="text-sm font-medium truncate">{svc.service_name.replace('hive-app-', '').replace('hive-db-', 'db:')}</span>
					</div>
					<span class="text-xs font-mono" style="color: var(--color-text-muted);">{svc.running}/{svc.replicas}</span>
				</div>
			{/each}
		</div>
	{/if}
</div>
