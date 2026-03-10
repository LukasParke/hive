<script lang="ts">
	import type { PrometheusNodeCurrent, SwarmNode } from '$lib/types';
	import GaugeRing from './GaugeRing.svelte';
	import ProgressBar from './ProgressBar.svelte';
	import StatusDot from './StatusDot.svelte';

	let {
		hostname,
		nodeId,
		role = 'unknown',
		state = 'ready',
		addr = '',
		cores = 0,
		memTotal = 0,
		prom = null,
		href,
	}: {
		hostname: string;
		nodeId: string;
		role?: string;
		state?: string;
		addr?: string;
		cores?: number;
		memTotal?: number;
		prom?: PrometheusNodeCurrent | null;
		href?: string;
	} = $props();

	let isReady = $derived(state === 'ready');
	let nodeOk = $derived(isReady && (!prom || prom.up));
	let memPct = $derived(prom ? pct(prom.memUsed, prom.memTotal) : 0);
	let diskPct = $derived(prom ? pct(prom.diskUsed, prom.diskTotal) : 0);

	function pct(used: number, total: number): number {
		if (!total) return 0;
		return Math.round((used / total) * 100);
	}

	function formatBytes(bytes: number): string {
		if (!bytes || bytes <= 0) return '0 B';
		const k = 1024;
		const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
		const i = Math.floor(Math.log(bytes) / Math.log(k));
		return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
	}

	function formatUptime(seconds: number): string {
		const days = Math.floor(seconds / 86400);
		const hours = Math.floor((seconds % 86400) / 3600);
		if (days > 0) return `${days}d ${hours}h`;
		const mins = Math.floor((seconds % 3600) / 60);
		return `${hours}h ${mins}m`;
	}
</script>

<a class="node-card" href={href ?? `/nodes/${nodeId}`} style="{!nodeOk ? 'opacity: 0.7;' : ''}">
	<!-- Header -->
	<div class="node-card-header">
		<div class="node-card-name">
			<StatusDot status={nodeOk ? 'success' : 'danger'} />
			<h4>{hostname}</h4>
		</div>
		<div class="node-card-tags">
			<span class="node-tag">{role}</span>
			<span class="node-tag">{cores} cores</span>
		</div>
	</div>

	{#if prom}
		<!-- Gauges + Metrics -->
		<div class="node-card-gauges">
			<GaugeRing value={prom.cpuPct} size={52} strokeWidth={5} label="CPU" />
			<GaugeRing value={memPct} size={52} strokeWidth={5} label="RAM" />
			<div class="node-card-bars">
				<div class="bar-row">
					<div class="bar-header">
						<span>Disk</span>
						<span class="tabular">{formatBytes(prom.diskUsed)} / {formatBytes(prom.diskTotal)}</span>
					</div>
					<ProgressBar value={prom.diskUsed} max={prom.diskTotal} height={5} />
				</div>
				{#if (prom.loadAvg1 ?? 0) > 0}
					<div class="bar-row">
						<div class="bar-header">
							<span>Load</span>
							<span class="tabular">{(prom.loadAvg1).toFixed(2)}</span>
						</div>
						<ProgressBar value={prom.loadAvg1} max={cores || 1} height={5} />
					</div>
				{/if}
			</div>
		</div>

		<!-- Footer stats -->
		<div class="node-card-footer">
			<span class="tabular">{prom.containersRunning} containers</span>
			{#if (prom.tempCelsius ?? 0) > 0}
				<span class="tabular">{(prom.tempCelsius).toFixed(0)}°C</span>
			{/if}
			<span>up {formatUptime(prom.uptimeSeconds)}</span>
		</div>
	{:else}
		<!-- No prometheus data yet -->
		<div class="node-card-nodata">
			<div class="nodata-row">
				<span>Memory</span>
				<span>{formatBytes(memTotal)}</span>
			</div>
			{#if addr}
				<div class="nodata-row">
					<span>Address</span>
					<span class="font-mono text-xs">{addr}</span>
				</div>
			{/if}
			<div class="nodata-waiting">
				<span class="spinner" style="width: 10px; height: 10px; border-width: 1.5px;"></span>
				<span>Waiting for metrics...</span>
			</div>
		</div>
	{/if}
</a>

<style>
	.node-card {
		display: block;
		border-radius: var(--radius-lg);
		background-color: var(--color-surface);
		border: 1px solid var(--color-border);
		transition: transform 0.15s ease, box-shadow 0.2s ease, border-color 0.2s ease;
		text-decoration: none;
		color: inherit;
		overflow: hidden;
	}
	.node-card:hover {
		transform: translateY(-1px);
		border-color: var(--color-primary-border);
		box-shadow: var(--shadow-glow);
	}

	.node-card-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 1rem 1.25rem 0.5rem;
	}
	.node-card-name {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}
	.node-card-name h4 {
		font-weight: 600;
		font-size: var(--text-sm);
	}
	.node-card-tags {
		display: flex;
		align-items: center;
		gap: 0.375rem;
	}
	.node-tag {
		font-size: 0.625rem;
		padding: 0.125rem 0.375rem;
		border-radius: var(--radius-sm);
		text-transform: capitalize;
		background-color: var(--color-bg);
		color: var(--color-text-muted);
	}

	.node-card-gauges {
		display: flex;
		align-items: center;
		gap: 1rem;
		padding: 0.5rem 1.25rem 0.75rem;
	}
	.node-card-bars {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
	}
	.bar-row {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
	}
	.bar-header {
		display: flex;
		justify-content: space-between;
		font-size: 0.625rem;
		color: var(--color-text-muted);
	}

	.node-card-footer {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0.5rem 1.25rem;
		font-size: 0.6875rem;
		color: var(--color-text-muted);
		border-top: 1px solid var(--color-border);
	}

	.node-card-nodata {
		padding: 0.5rem 1.25rem 1rem;
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
		font-size: var(--text-sm);
		color: var(--color-text-muted);
	}
	.nodata-row {
		display: flex;
		justify-content: space-between;
	}
	.nodata-waiting {
		display: flex;
		align-items: center;
		gap: 0.375rem;
		padding-top: 0.5rem;
		font-size: var(--text-xs);
		font-style: italic;
		opacity: 0.7;
	}

	.tabular {
		font-variant-numeric: tabular-nums;
	}

	@media (max-width: 768px) {
		.node-card-header,
		.node-card-gauges,
		.node-card-footer {
			padding-left: 1rem;
			padding-right: 1rem;
		}
	}
</style>
