<script lang="ts">
	import { onMount } from 'svelte';
	import { metricsStore } from '$lib/stores/metrics.svelte';
	import {
		GaugeRing, DoughnutChart, Badge, NodeCard, StatCard, Panel, Alert, StatusDot
	} from '$lib/components';
	import type {
		ServiceHealth,
		PrometheusClusterSummary,
		PrometheusNodeCurrent,
		SwarmNode,
		AuditLogEntry
	} from '$lib/types';

	let { data } = $props<{
		status?: { status: string; role: string; node_count: number; multi_node: boolean; nats: string };
		swarmNodes: SwarmNode[];
		cluster: PrometheusClusterSummary | null;
		promNodes: PrometheusNodeCurrent[];
		serviceHealth: ServiceHealth[];
		recentAudit: AuditLogEntry[];
		registryStatus: { status: string; images_count?: number } | null;
	}>();

	onMount(() => {
		metricsStore.seedFromSSR(data.cluster, data.promNodes, data.serviceHealth);
		return metricsStore.subscribe();
	});

	const ms = $derived(metricsStore.state);

	let cluster = $derived(ms.cluster ?? data.cluster);
	let hasPromData = $derived(cluster != null && (cluster.nodesUp ?? 0) > 0);
	let promNodes = $derived(ms.nodes.length > 0 ? ms.nodes : (Array.isArray(data.promNodes) ? data.promNodes : []));
	let serviceHealth = $derived(ms.serviceHealth.length > 0 ? ms.serviceHealth : (Array.isArray(data.serviceHealth) ? data.serviceHealth : []));
	let topContainers = $derived(ms.topContainers);

	interface MergedNode {
		id: string;
		hostname: string;
		role: string;
		state: string;
		addr: string;
		cores: number;
		memTotal: number;
		prom: PrometheusNodeCurrent | null;
	}

	let mergedNodes: MergedNode[] = $derived.by(() => {
		const promByHostname = new Map<string, PrometheusNodeCurrent>();
		for (const p of promNodes) {
			promByHostname.set(p.hostname, p);
		}

		if (data.swarmNodes.length > 0) {
			return data.swarmNodes.map((node: SwarmNode): MergedNode => {
				const hostname = node.Description.Hostname;
				const prom = promByHostname.get(hostname) ?? null;
				return {
					id: node.ID,
					hostname,
					role: node.Spec.Role,
					state: node.Status.State,
					addr: node.Status.Addr,
					cores: prom?.cores ?? Math.round(node.Description.Resources.NanoCPUs / 1e9),
					memTotal: prom?.memTotal ?? node.Description.Resources.MemoryBytes,
					prom
				};
			});
		}

		return promNodes.map((p: PrometheusNodeCurrent): MergedNode => ({
			id: p.nodeId,
			hostname: p.hostname,
			role: 'unknown',
			state: p.up ? 'ready' : 'down',
			addr: '',
			cores: p.cores,
			memTotal: p.memTotal,
			prom: p
		}));
	});

	let alerts = $derived.by(() => {
		const a: string[] = [];
		for (const node of mergedNodes) {
			const p = node.prom;
			if (p && !p.up) {
				a.push(`${node.hostname}: Node exporter unreachable`);
			}
			if (p && typeof p.cpuPct === 'number' && p.cpuPct > 90) {
				a.push(`${node.hostname}: CPU at ${p.cpuPct.toFixed(0)}%`);
			}
			if (p && typeof p.diskUsed === 'number' && typeof p.diskTotal === 'number') {
				const diskPct = pct(p.diskUsed, p.diskTotal);
				if (diskPct > 85) a.push(`${node.hostname}: Disk at ${diskPct}%`);
			}
		}
		for (const svc of serviceHealth) {
			if (svc && !svc.healthy && svc.service_name) {
				const name = String(svc.service_name).replace('hive-app-', '');
				a.push(`Service ${name}: ${svc.running ?? 0}/${svc.replicas ?? 0} replicas`);
			}
		}
		return a;
	});

	let healthySvcCount = $derived(serviceHealth.filter((s: ServiceHealth) => s.healthy).length);
	let unhealthySvcCount = $derived(serviceHealth.filter((s: ServiceHealth) => !s.healthy).length);

	let totalRAM = $derived(hasPromData && cluster ? cluster.totalRAM ?? 0 : mergedNodes.reduce((s: number, n: MergedNode) => s + n.memTotal, 0));
	let usedRAM = $derived(promNodes.reduce((s: number, n: PrometheusNodeCurrent) => s + (n.memUsed ?? 0), 0));
	let realMemPct = $derived(totalRAM > 0 ? pct(usedRAM, totalRAM) : 0);

	let diskSegments = $derived.by(() => {
		if (!cluster || !cluster.totalDisk) return [];
		return [
			{ value: cluster.usedDisk, label: 'Used', color: 'var(--color-primary)' },
			{ value: cluster.totalDisk - cluster.usedDisk, label: 'Free', color: 'var(--color-border)' },
		];
	});

	function formatBytes(bytes: number): string {
		if (!bytes || bytes <= 0) return '0 B';
		const k = 1024;
		const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
		const i = Math.floor(Math.log(bytes) / Math.log(k));
		return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
	}

	function pct(used: number, total: number): number {
		if (!total) return 0;
		return Math.round((used / total) * 100);
	}

	function barColor(value: number): string {
		if (value > 85) return 'var(--color-danger)';
		if (value > 60) return 'var(--color-primary)';
		return 'var(--color-success)';
	}

	function timeAgo(dateStr: string): string {
		const date = new Date(dateStr);
		const seconds = Math.floor((Date.now() - date.getTime()) / 1000);
		if (seconds < 60) return 'just now';
		if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
		if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
		return `${Math.floor(seconds / 86400)}d ago`;
	}
</script>

<svelte:head><title>Dashboard | Hive</title></svelte:head>

<div class="max-w-7xl mx-auto">
	<!-- Header -->
	<div class="page-header">
		<div>
			<h2 class="page-title">Dashboard</h2>
			<p class="page-subtitle">Cluster overview and monitoring</p>
		</div>
		<div class="flex items-center gap-3">
			{#if ms.connected}
				<Badge variant="success" dot>Live</Badge>
			{:else if ms.error}
				<Badge variant="danger" dot>Disconnected</Badge>
			{:else}
				<Badge variant="primary" dot>Connecting</Badge>
			{/if}
		</div>
	</div>

	<!-- Alerts -->
	{#if alerts.length > 0}
		<Alert variant="danger" class="mb-6">
			<div class="flex items-center gap-2 mb-2">
				<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--color-danger)" stroke-width="2"><path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
				<h3 class="font-semibold text-sm" style="color: var(--color-danger);">Active Alerts ({alerts.length})</h3>
			</div>
			<ul class="space-y-1 text-sm">
				{#each alerts as alert}
					<li style="color: #fca5a5;">{alert}</li>
				{/each}
			</ul>
		</Alert>
	{/if}

	<!-- Cluster Summary Row -->
	{#if hasPromData || mergedNodes.length > 0}
		<div class="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-6">
			<!-- CPU Gauge -->
			<StatCard>
				<div class="stat-inner">
					<GaugeRing value={hasPromData && cluster ? cluster.avgCPU ?? 0 : 0} size={72} strokeWidth={7} label="CPU" />
					<div class="stat-text">
						<span class="stat-big" style="color: {barColor(hasPromData && cluster ? cluster.avgCPU ?? 0 : 0)};">
							{hasPromData && cluster ? (cluster.avgCPU ?? 0).toFixed(1) : '--'}%
						</span>
						<span class="stat-sub">{hasPromData && cluster ? cluster.totalCores ?? 0 : mergedNodes.reduce((s, n) => s + n.cores, 0)} cores total</span>
					</div>
				</div>
			</StatCard>

			<!-- Memory -->
			<StatCard>
				<div class="stat-inner">
					<GaugeRing value={realMemPct} size={72} strokeWidth={7} label="RAM" />
					<div class="stat-text">
						<span class="stat-big">{formatBytes(usedRAM)}</span>
						<span class="stat-sub">of {formatBytes(totalRAM)}</span>
					</div>
				</div>
			</StatCard>

			<!-- Disk -->
			<StatCard>
				<div class="stat-inner">
					{#if hasPromData && cluster && (cluster.totalDisk ?? 0) > 0}
						<DoughnutChart segments={diskSegments} size={72} strokeWidth={10}
							centerValue="{pct(cluster.usedDisk ?? 0, cluster.totalDisk ?? 1)}%"
							centerLabel="disk" />
						<div class="stat-text">
							<span class="stat-big">{formatBytes(cluster.usedDisk ?? 0)}</span>
							<span class="stat-sub">of {formatBytes(cluster.totalDisk ?? 0)}</span>
						</div>
					{:else}
						<div class="text-center w-full">
							<span class="stat-big">--</span>
							<span class="stat-sub">No disk data</span>
						</div>
					{/if}
				</div>
			</StatCard>

			<!-- Cluster Info -->
			<StatCard>
				<div class="grid grid-cols-2 gap-3 w-full">
					<div class="mini-stat">
						<span class="mini-val">{hasPromData && cluster ? cluster.nodesUp ?? 0 : mergedNodes.length}</span>
						<span class="mini-label">Nodes Up</span>
					</div>
					<div class="mini-stat">
						<span class="mini-val">{hasPromData && cluster ? cluster.containers ?? 0 : '--'}</span>
						<span class="mini-label">Containers</span>
					</div>
					<div class="mini-stat">
						<span class="mini-val" style="color: var(--color-success);">{healthySvcCount}</span>
						<span class="mini-label">Healthy</span>
					</div>
					<div class="mini-stat">
						<span class="mini-val" style="color: {unhealthySvcCount > 0 ? 'var(--color-danger)' : 'var(--color-text-muted)'};">{unhealthySvcCount}</span>
						<span class="mini-label">Unhealthy</span>
					</div>
				</div>
			</StatCard>
		</div>
	{/if}

	<!-- Quick Actions -->
	<div class="quick-actions mb-6">
		<a href="/catalog" class="quick-action-btn">
			<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>
			Deploy from Catalog
		</a>
		<a href="/projects" class="quick-action-btn">
			<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M12 5v14M5 12h14"/></svg>
			New Project
		</a>
		<a href="/backups" class="quick-action-btn">
			<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><polyline points="1 4 1 10 7 10"/><path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"/></svg>
			Backups
		</a>
		<a href="/routing" class="quick-action-btn">
			<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M16 3h5v5"/><line x1="4" y1="20" x2="21" y2="3"/><path d="M21 16v5h-5"/><line x1="15" y1="15" x2="21" y2="21"/><line x1="4" y1="4" x2="9" y2="9"/></svg>
			Routing
		</a>
	</div>

	<!-- Nodes Grid -->
	{#if mergedNodes.length > 0}
		<div class="flex items-center gap-3 mb-4">
			<h3 class="section-heading">Nodes</h3>
			{#if !cluster || cluster.nodesUp === 0}
				<Badge variant="primary">
					<span class="spinner" style="width: 10px; height: 10px; border-width: 1.5px;"></span>
					Waiting for Prometheus
				</Badge>
			{/if}
		</div>
		<div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4 mb-8">
			{#each mergedNodes as node}
				<NodeCard
					hostname={node.hostname}
					nodeId={node.id}
					role={node.role}
					state={node.state}
					addr={node.addr}
					cores={node.cores}
					memTotal={node.memTotal}
					prom={node.prom}
				/>
			{/each}
		</div>
	{/if}

	<!-- Two-column: Service Health + Top Containers -->
	<div class="grid grid-cols-1 lg:grid-cols-2 gap-6 mb-8">
		<!-- Service Health -->
		<Panel title="Service Health">
			{#snippet headerRight()}
				<span class="text-xs tabular" style="color: var(--color-text-muted);">{serviceHealth.length} services</span>
			{/snippet}
			{#if serviceHealth.length > 0}
				<div class="space-y-1">
					{#each serviceHealth as svc}
						<div class="svc-row">
							<div class="flex items-center gap-2 min-w-0">
								<StatusDot status={svc.healthy ? 'success' : 'danger'} />
								<span class="text-sm font-medium truncate">{String(svc.service_name ?? '').replace('hive-app-', '').replace('hive-db-', 'db:')}</span>
								{#if svc.is_global}
									<Badge variant="neutral">global</Badge>
								{/if}
							</div>
							<span class="text-xs font-mono tabular" style="color: {svc.healthy ? 'var(--color-success)' : 'var(--color-danger)'};">
								{svc.running ?? 0}/{svc.replicas ?? 0}
							</span>
						</div>
					{/each}
				</div>
			{:else}
				<p class="text-sm text-center py-6" style="color: var(--color-text-muted);">No managed services detected</p>
			{/if}
		</Panel>

		<!-- Top Containers -->
		<Panel title="Top Containers by CPU">
			{#if topContainers.length > 0}
				<div style="overflow-x: auto;">
				<table class="hive-table">
					<thead>
						<tr>
							<th class="text-left">Service</th>
							<th class="text-right">CPU</th>
							<th class="text-right">Memory</th>
						</tr>
					</thead>
					<tbody>
						{#each topContainers.slice(0, 8) as c}
							<tr>
								<td class="font-mono text-xs truncate max-w-[200px]">{c.name}</td>
								<td class="text-right tabular text-xs">{(c.cpuPct ?? 0).toFixed(1)}%</td>
								<td class="text-right tabular text-xs">{formatBytes(c.memBytes ?? 0)}</td>
							</tr>
						{/each}
					</tbody>
				</table>
				</div>
			{:else}
				<p class="text-sm text-center py-6" style="color: var(--color-text-muted);">No container metrics yet</p>
			{/if}
		</Panel>
	</div>

	<!-- System Status Row -->
	<div class="grid grid-cols-1 lg:grid-cols-2 gap-6 mb-8">
		<!-- Registry & System -->
		<Panel title="System Status">
			<div class="space-y-3">
				<div class="sys-row">
					<span class="sys-label">NATS</span>
					{#if data.status}
						<Badge variant={data.status.nats === 'connected' ? 'success' : 'danger'} dot>
							{data.status.nats}
						</Badge>
					{:else}
						<Badge variant="neutral">Unknown</Badge>
					{/if}
				</div>
				<div class="sys-row">
					<span class="sys-label">Registry</span>
					{#if data.registryStatus}
						<Badge variant={data.registryStatus.status === 'ok' || data.registryStatus.status === 'available' ? 'success' : 'warning'} dot>
							{data.registryStatus.status}{data.registryStatus.images_count != null ? ` (${data.registryStatus.images_count} images)` : ''}
						</Badge>
					{:else}
						<Badge variant="neutral">Not available</Badge>
					{/if}
				</div>
				<div class="sys-row">
					<span class="sys-label">Swarm Role</span>
					<Badge variant="primary">{data.status?.role ?? 'unknown'}</Badge>
				</div>
				<div class="sys-row">
					<span class="sys-label">Multi-Node</span>
					<Badge variant={data.status?.multi_node ? 'success' : 'neutral'}>
						{data.status?.multi_node ? 'Yes' : 'Single node'}
					</Badge>
				</div>
			</div>
		</Panel>

		<!-- Recent Activity -->
		<Panel title="Recent Activity">
			{#if data.recentAudit && data.recentAudit.length > 0}
				<div class="space-y-2">
					{#each data.recentAudit.slice(0, 6) as entry}
						<div class="audit-row">
							<div class="flex items-center gap-2 min-w-0">
								<span class="audit-action">{entry.action}</span>
								<span class="text-xs truncate" style="color: var(--color-text-muted);">
									{entry.resource_type}{entry.detail ? `: ${entry.detail}` : ''}
								</span>
							</div>
							<span class="text-xs tabular" style="color: var(--color-text-disabled);">
								{timeAgo(entry.created_at)}
							</span>
						</div>
					{/each}
				</div>
				<div class="mt-3 pt-3" style="border-top: 1px solid var(--color-border);">
					<a href="/audit" class="text-xs hover:underline" style="color: var(--color-primary);">View all activity</a>
				</div>
			{:else}
				<p class="text-sm text-center py-6" style="color: var(--color-text-muted);">No recent activity</p>
			{/if}
		</Panel>
	</div>
</div>

<style>
	.stat-inner {
		display: flex;
		align-items: center;
		gap: var(--space-md);
	}
	.stat-text {
		display: flex;
		flex-direction: column;
	}
	.stat-big {
		font-size: 1.25rem;
		font-weight: 700;
		font-variant-numeric: tabular-nums;
		line-height: 1.2;
	}
	.stat-sub {
		font-size: 0.6875rem;
		color: var(--color-text-muted);
		margin-top: 0.125rem;
	}

	.mini-stat {
		display: flex;
		flex-direction: column;
		align-items: center;
		text-align: center;
	}
	.mini-val {
		font-size: 1.125rem;
		font-weight: 700;
		font-variant-numeric: tabular-nums;
		line-height: 1.2;
	}
	.mini-label {
		font-size: 0.625rem;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--color-text-muted);
		margin-top: 0.125rem;
	}

	.quick-actions {
		display: flex;
		gap: 0.5rem;
		flex-wrap: wrap;
	}
	.quick-action-btn {
		display: inline-flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.5rem 0.875rem;
		font-size: 0.8125rem;
		font-weight: 500;
		border-radius: var(--radius-md);
		background-color: var(--color-surface);
		border: 1px solid var(--color-border);
		color: var(--color-text-secondary);
		text-decoration: none;
		transition: all var(--transition-base);
	}
	.quick-action-btn:hover {
		border-color: var(--color-primary-border);
		color: var(--color-text);
		background-color: var(--color-surface-hover);
	}

	.section-heading {
		font-size: var(--text-base);
		font-weight: 600;
	}

	.svc-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0.375rem 0;
		border-bottom: 1px solid var(--color-border);
	}
	.svc-row:last-child {
		border-bottom: none;
	}

	.tabular {
		font-variant-numeric: tabular-nums;
	}

	.sys-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
	}
	.sys-label {
		font-size: var(--text-sm);
		color: var(--color-text-muted);
	}

	.audit-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-sm);
	}
	.audit-action {
		font-size: 0.6875rem;
		font-weight: 600;
		text-transform: uppercase;
		padding: 0.125rem 0.375rem;
		border-radius: var(--radius-sm);
		background-color: var(--color-primary-dim);
		color: var(--color-primary);
		flex-shrink: 0;
	}

	@media (max-width: 768px) {
		.quick-action-btn {
			min-height: 44px;
			flex: 1 1 auto;
			justify-content: center;
		}
		.stat-big {
			font-size: 1rem;
		}
	}
</style>
