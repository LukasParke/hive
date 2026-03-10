<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import type { NodeUpdateStatus, PrometheusNodeHistory } from '$lib/types';
	import { GaugeRing, AreaChart, Button, Badge, Panel, Alert, StatusDot, ProgressBar } from '$lib/components';
	import { metricsStore } from '$lib/stores/metrics.svelte';
	import { updatesStore } from '$lib/stores/updates.svelte';

	let { data: pageData } = $props();

	let historyRange = $state('1h');
	let historyOverride = $state<PrometheusNodeHistory | null>(null);
	let actionLoading = $state('');
	let actionMessage = $state('');
	let actionError = $state('');
	let editingLabels = $state(false);
	let labelDraft = $state<Record<string, string>>({});
	let newLabelKey = $state('');
	let newLabelValue = $state('');

	interface ContainerMetric {
		name: string;
		image: string;
		instance: string;
		cpuPct: number;
		memBytes: number;
	}
	let containers = $state<ContainerMetric[]>([]);
	let nodeUpdateStatus = $state<NodeUpdateStatus | null>(null);
	let updateCheckLoading = $state(false);
	let showUpdateLog = $state(false);

	$effect(() => {
		let cancelled = false;
		async function fetchContainers() {
			try {
				const res = await fetch(`/api/v1/nodes/${encodeURIComponent(pageData.hostname)}/containers`);
				if (res.ok && !cancelled) containers = await res.json();
			} catch { /* ignore */ }
		}
		fetchContainers();
		const timer = setInterval(fetchContainers, 15_000);
		return () => { cancelled = true; clearInterval(timer); };
	});

	let metricsUnsub: (() => void) | undefined;
	onMount(() => {
		updatesStore.subscribe();
		loadUpdateStatus();
		metricsUnsub = metricsStore.subscribe();
		return () => {
			metricsUnsub?.();
			updatesStore.unsubscribe();
		};
	});

	let liveUpdateOp = $derived(updatesStore.state.activeNodeOperations.get(pageData.hostname));
	let updateLog = $derived(updatesStore.state.nodeOutputLog.get(pageData.hostname) ?? []);

	async function loadUpdateStatus() {
		try {
			nodeUpdateStatus = await api.updatesNodeDetail(pageData.hostname);
		} catch { /* node may not have reported yet */ }
	}

	async function checkForUpdates() {
		updateCheckLoading = true;
		try {
			const result = await api.checkNodeUpdates(pageData.hostname);
			if (result) nodeUpdateStatus = result;
		} catch (e: any) {
			actionError = e.message;
		}
		updateCheckLoading = false;
	}

	async function applyUpdates(securityOnly = false) {
		actionLoading = securityOnly ? 'security_upgrade' : 'full_upgrade';
		actionError = '';
		updatesStore.clearNodeLog(pageData.hostname);
		showUpdateLog = true;
		try {
			await api.applyNodeUpdates(pageData.hostname, { security_only: securityOnly });
			actionMessage = 'Update triggered';
			setTimeout(() => actionMessage = '', 3000);
		} catch (e: any) {
			actionError = e.message;
		}
		actionLoading = '';
	}

	async function rebootNode() {
		if (!confirm('Reboot this node? It will temporarily go offline.')) return;
		actionLoading = 'reboot';
		try {
			await api.applyNodeUpdates(pageData.hostname, { action: 'reboot' });
			actionMessage = 'Reboot initiated';
			setTimeout(() => actionMessage = '', 3000);
		} catch (e: any) {
			actionError = e.message;
		}
		actionLoading = '';
	}

	let swarmNodeOverride = $state<typeof pageData.swarmNode>(null);
	let swarmNode = $derived(swarmNodeOverride ?? pageData.swarmNode);
	let nodeLabels = $derived(swarmNode?.Spec?.Labels ?? {});

	let liveNode = $derived(
		metricsStore.state.nodes.find(n => n.hostname === pageData.hostname) ?? pageData.promNode
	);

	let history = $derived(historyOverride ?? pageData.history);
	let cpuHistoryPts = $derived(history?.cpu ?? []);
	let memHistoryPts = $derived(history?.mem ?? []);

	async function setAvailability(availability: string) {
		if (!swarmNode) return;
		actionLoading = 'availability';
		actionError = '';
		try {
			await api.updateNodeAvailability(swarmNode.ID, availability);
			const data = await api.listNodes();
			const updated = (data.nodes ?? []).find((n) => n.ID === swarmNode!.ID);
			if (updated) swarmNodeOverride = updated;
			actionMessage = `Node set to ${availability}`;
			setTimeout(() => actionMessage = '', 3000);
		} catch (e: any) {
			actionError = e.message;
		} finally {
			actionLoading = '';
		}
	}

	async function setRole(role: string) {
		if (!swarmNode) return;
		actionLoading = 'role';
		actionError = '';
		try {
			await api.updateNodeRole(swarmNode.ID, role);
			const data = await api.listNodes();
			const updated = (data.nodes ?? []).find((n) => n.ID === swarmNode!.ID);
			if (updated) swarmNodeOverride = updated;
			actionMessage = `Node role changed to ${role}`;
			setTimeout(() => actionMessage = '', 3000);
		} catch (e: any) {
			actionError = e.message;
		} finally {
			actionLoading = '';
		}
	}

	async function triggerMaintenance(action: string) {
		if (!swarmNode) return;
		actionLoading = action;
		actionError = '';
		try {
			await api.nodeMaintenanceAction(swarmNode.ID, action);
			actionMessage = `${action.replace('_', ' ')} triggered`;
			setTimeout(() => actionMessage = '', 3000);
		} catch (e: any) {
			actionError = e.message;
		} finally {
			actionLoading = '';
		}
	}

	function startEditLabels() {
		labelDraft = { ...nodeLabels };
		newLabelKey = '';
		newLabelValue = '';
		editingLabels = true;
	}

	function addLabel() {
		if (newLabelKey.trim()) {
			labelDraft[newLabelKey.trim()] = newLabelValue.trim();
			newLabelKey = '';
			newLabelValue = '';
		}
	}

	function removeLabel(key: string) {
		delete labelDraft[key];
		labelDraft = { ...labelDraft };
	}

	async function saveLabels() {
		if (!swarmNode) return;
		actionLoading = 'labels';
		actionError = '';
		try {
			await api.updateNodeLabels(swarmNode.ID, labelDraft);
			const data = await api.listNodes();
			const updated = (data.nodes ?? []).find((n) => n.ID === swarmNode!.ID);
			if (updated) swarmNodeOverride = updated;
			editingLabels = false;
			actionMessage = 'Labels updated';
			setTimeout(() => actionMessage = '', 3000);
		} catch (e: any) {
			actionError = e.message;
		} finally {
			actionLoading = '';
		}
	}

	let memPct = $derived(liveNode ? pct(liveNode.memUsed, liveNode.memTotal) : 0);
	let diskPct = $derived(liveNode ? pct(liveNode.diskUsed, liveNode.diskTotal) : 0);

	async function loadHistory() {
		try {
			const res = await fetch(`/api/v1/nodes/${encodeURIComponent(pageData.hostname)}/metrics/history?range=${historyRange}`);
			if (res.ok) {
				historyOverride = await res.json();
			}
		} catch { /* ignore */ }
	}

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

	function barColor(v: number): string {
		if (v > 85) return 'var(--color-danger)';
		if (v > 60) return 'var(--color-primary)';
		return 'var(--color-success)';
	}

	function formatUptime(seconds: number): string {
		const days = Math.floor(seconds / 86400);
		const hours = Math.floor((seconds % 86400) / 3600);
		const mins = Math.floor((seconds % 3600) / 60);
		if (days > 0) return `${days}d ${hours}h ${mins}m`;
		return `${hours}h ${mins}m`;
	}
</script>

<svelte:head><title>{pageData.hostname} | Hive</title></svelte:head>

<div class="max-w-7xl mx-auto">
	{#if liveNode}
		<!-- Node Header -->
		<div class="page-header">
			<div class="flex items-center gap-3 flex-wrap">
				<h2 class="page-title">{liveNode.hostname}</h2>
				<Badge variant={liveNode.up ? 'success' : 'danger'} dot>
					{liveNode.up ? 'Online' : 'Offline'}
				</Badge>
				{#if swarmNode}
					<Badge variant="neutral">{swarmNode.Spec.Role}</Badge>
				{/if}
			</div>
			<div class="flex items-center gap-2">
				{#if metricsStore.state.connected}
					<Badge variant="success" dot>Live</Badge>
				{/if}
			</div>
		</div>

		<!-- Resource Gauges -->
		<div class="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
			<div class="gauge-card">
				<GaugeRing value={liveNode.cpuPct} size={80} strokeWidth={7} label="CPU" />
				<p class="gauge-sub">{liveNode.cores} cores &middot; {liveNode.cpuPct.toFixed(1)}%</p>
			</div>
			<div class="gauge-card">
				<GaugeRing value={memPct} size={80} strokeWidth={7} label="RAM" />
				<p class="gauge-sub">{formatBytes(liveNode.memUsed)} / {formatBytes(liveNode.memTotal)}</p>
			</div>
			<div class="gauge-card">
				<GaugeRing value={diskPct} size={80} strokeWidth={7} label="Disk" />
				<p class="gauge-sub">{formatBytes(liveNode.diskUsed)} / {formatBytes(liveNode.diskTotal)}</p>
			</div>
			<div class="gauge-card">
				<div class="text-center space-y-2 w-full">
					<div class="grid grid-cols-2 gap-2">
						<div>
							<span class="detail-label">Load</span>
							<p class="text-sm font-mono tabular">{(liveNode.loadAvg1 ?? 0).toFixed(2)}</p>
						</div>
						<div>
							<span class="detail-label">Uptime</span>
							<p class="text-sm">{formatUptime(liveNode.uptimeSeconds)}</p>
						</div>
						{#if (liveNode.tempCelsius ?? 0) > 0}
							<div>
								<span class="detail-label">Temp</span>
								<p class="text-base font-bold tabular" style="color: {barColor((liveNode.tempCelsius ?? 0) > 80 ? 90 : (liveNode.tempCelsius ?? 0) > 65 ? 70 : 30)};">{(liveNode.tempCelsius ?? 0).toFixed(0)}°C</p>
							</div>
						{/if}
						<div>
							<span class="detail-label">Containers</span>
							<p class="text-sm tabular">{liveNode.containersRunning}</p>
						</div>
					</div>
				</div>
			</div>
		</div>

		<!-- Feedback Messages -->
		{#if actionMessage}
			<Alert variant="success" class="mb-4">
				<p class="text-sm" style="color: var(--color-success);">{actionMessage}</p>
			</Alert>
		{/if}
		{#if actionError}
			<Alert variant="danger" class="mb-4">
				<p class="text-sm" style="color: var(--color-danger);">{actionError}</p>
			</Alert>
		{/if}

		<!-- Historical Charts -->
		{#if cpuHistoryPts.length > 1 || memHistoryPts.length > 1}
			<Panel title="Resource History" class="mb-6">
				{#snippet headerRight()}
					<div class="flex items-center gap-1">
						{#each ['1h', '6h', '24h', '7d'] as range}
							<button class="range-btn" class:active={historyRange === range}
								onclick={() => { historyRange = range; loadHistory(); }}>
								{range}
							</button>
						{/each}
					</div>
				{/snippet}

				<div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
					{#if cpuHistoryPts.length > 1}
						<div>
							<AreaChart data={cpuHistoryPts} color="var(--color-success)" label="CPU Usage" unit="%" min={0} max={100} height={140} />
						</div>
					{/if}
					{#if memHistoryPts.length > 1}
						<div>
							<AreaChart data={memHistoryPts} color="var(--color-info)" label="Memory Usage" unit="%" min={0} max={100} height={140} />
						</div>
					{/if}
				</div>
			</Panel>
		{/if}

		<!-- Swarm Info -->
		{#if swarmNode}
			<Panel title="Swarm Details" class="mb-6">
				<div class="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
					<div>
						<p class="detail-label">Node ID</p>
						<p class="font-mono text-xs truncate">{swarmNode.ID}</p>
					</div>
					<div>
						<p class="detail-label">Role</p>
						<p class="capitalize">{swarmNode.Spec.Role}</p>
					</div>
					<div>
						<p class="detail-label">State</p>
						<p class="capitalize" style="color: {swarmNode.Status.State === 'ready' ? 'var(--color-success)' : 'var(--color-danger)'};">{swarmNode.Status.State}</p>
					</div>
					<div>
						<p class="detail-label">Address</p>
						<p class="font-mono text-xs">{swarmNode.Status.Addr}</p>
					</div>
					<div>
						<p class="detail-label">Architecture</p>
						<p>{swarmNode.Description.Platform.Architecture}</p>
					</div>
					<div>
						<p class="detail-label">OS</p>
						<p>{swarmNode.Description.Platform.OS}</p>
					</div>
					<div>
						<p class="detail-label">Availability</p>
						<p class="capitalize">{swarmNode.Spec.Availability}</p>
					</div>
				</div>
			</Panel>

			<!-- Node Actions -->
			<Panel title="Node Actions" class="mb-6">
				<div class="grid grid-cols-1 md:grid-cols-2 gap-6">
					<div>
						<p class="detail-label mb-2">Availability</p>
						<div class="flex gap-2">
							{#each ['active', 'drain', 'pause'] as avail}
								<Button
									size="sm"
									variant={swarmNode.Spec.Availability === avail ? 'primary' : 'secondary'}
									onclick={() => setAvailability(avail)}
									disabled={actionLoading === 'availability' || swarmNode.Spec.Availability === avail}>
									{avail}
								</Button>
							{/each}
						</div>
					</div>
					<div>
						<p class="detail-label mb-2">Role</p>
						<div class="flex gap-2">
							{#each ['manager', 'worker'] as role}
								<Button
									size="sm"
									variant={swarmNode.Spec.Role === role ? 'primary' : 'secondary'}
									onclick={() => setRole(role)}
									disabled={actionLoading === 'role' || swarmNode.Spec.Role === role}>
									{role}
								</Button>
							{/each}
						</div>
					</div>
				</div>
			</Panel>

			<!-- Labels -->
			<Panel title="Labels" class="mb-6">
				{#snippet headerRight()}
					{#if !editingLabels}
						<Button size="sm" onclick={startEditLabels}>Edit Labels</Button>
					{/if}
				{/snippet}

				{#if editingLabels}
					<div class="space-y-2 mb-3">
						{#each Object.entries(labelDraft) as [key, value]}
							<div class="flex items-center gap-2">
								<span class="flex-1 text-xs font-mono px-2 py-1.5 rounded" style="background-color: var(--color-bg); color: var(--color-text-muted);">{key}</span>
								<input type="text" bind:value={labelDraft[key]} class="flex-1 text-xs px-2 py-1.5 rounded outline-none font-mono" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />
								<Button size="sm" variant="danger" onclick={() => removeLabel(key)}>Remove</Button>
							</div>
						{/each}
						<div class="flex items-center gap-2">
							<input type="text" bind:value={newLabelKey} placeholder="key" class="flex-1 text-xs px-2 py-1.5 rounded outline-none font-mono" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />
							<input type="text" bind:value={newLabelValue} placeholder="value" class="flex-1 text-xs px-2 py-1.5 rounded outline-none font-mono" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />
							<Button size="sm" variant="primary" onclick={addLabel}>Add</Button>
						</div>
					</div>
					<div class="flex gap-2 justify-end">
						<Button size="sm" variant="ghost" onclick={() => editingLabels = false}>Cancel</Button>
						<Button size="sm" variant="primary" onclick={saveLabels} disabled={actionLoading === 'labels'} loading={actionLoading === 'labels'}>Save Labels</Button>
					</div>
				{:else}
					{#if Object.keys(nodeLabels).length > 0}
						<div class="flex flex-wrap gap-2">
							{#each Object.entries(nodeLabels) as [key, value]}
								<span class="tag">{key}{value ? `=${value}` : ''}</span>
							{/each}
						</div>
					{:else}
						<p class="text-xs" style="color: var(--color-text-muted);">No custom labels</p>
					{/if}
				{/if}
			</Panel>

			<!-- System Updates -->
			<Panel title="System Updates" class="mb-6">
				{#snippet headerRight()}
					<Button size="sm" variant="secondary" onclick={checkForUpdates} disabled={updateCheckLoading}>
						{updateCheckLoading ? 'Checking...' : 'Check for Updates'}
					</Button>
				{/snippet}

				{#if nodeUpdateStatus}
					<div class="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm mb-4">
						<div>
							<p class="detail-label">OS</p>
							<p class="text-xs">{nodeUpdateStatus.os_info || 'Linux'}</p>
						</div>
						<div>
							<p class="detail-label">Kernel</p>
							<p class="font-mono text-xs">{nodeUpdateStatus.kernel_version || 'unknown'}</p>
						</div>
						<div>
							<p class="detail-label">Package Manager</p>
							<p class="text-xs">{nodeUpdateStatus.package_manager || 'unknown'}</p>
						</div>
						<div>
							<p class="detail-label">Reboot Required</p>
							<p class="text-xs" style="color: {nodeUpdateStatus.reboot_required ? 'var(--color-danger)' : 'var(--color-success)'}">
								{nodeUpdateStatus.reboot_required ? 'Yes' : 'No'}
							</p>
						</div>
					</div>

					<div class="flex items-center gap-3 mb-4">
						{#if nodeUpdateStatus.pending_count === 0}
							<Badge variant="success">System is up to date</Badge>
						{:else}
							<Badge variant="warning">{nodeUpdateStatus.pending_count} updates available</Badge>
							{#if nodeUpdateStatus.security_count > 0}
								<Badge variant="danger">{nodeUpdateStatus.security_count} security</Badge>
							{/if}
						{/if}
						{#if nodeUpdateStatus.reboot_required}
							<Badge variant="info">Reboot required</Badge>
						{/if}
					</div>

					{#if nodeUpdateStatus.pending_packages && nodeUpdateStatus.pending_packages.length > 0}
						<div class="pkg-list mb-4" style="max-height: 250px; overflow-y: auto; font-size: 0.8rem;">
							<div class="pkg-list-header">
								<span>Package</span><span>Current</span><span>Available</span><span>Type</span>
							</div>
							{#each nodeUpdateStatus.pending_packages as pkg}
								<div class="pkg-list-row" class:security-row={pkg.is_security}>
									<span class="font-mono">{pkg.name}</span>
									<span class="font-mono" style="color: var(--color-text-muted);">{pkg.current_version}</span>
									<span class="font-mono" style="color: var(--color-success);">{pkg.new_version}</span>
									<span>{pkg.is_security ? 'Security' : 'Standard'}</span>
								</div>
							{/each}
						</div>
					{/if}
				{:else}
					<p class="text-xs mb-4" style="color: var(--color-text-muted);">No update data yet. Click "Check for Updates" to scan.</p>
				{/if}

				{#if liveUpdateOp}
					<div class="update-op-bar mb-4">
						<div class="flex justify-between text-xs mb-1">
							<span class="font-semibold">{liveUpdateOp.action}</span>
							<span style="color: {liveUpdateOp.status === 'completed' ? 'var(--color-success)' : liveUpdateOp.status === 'failed' ? 'var(--color-danger)' : 'var(--color-warning)'}">
								{liveUpdateOp.status}
							</span>
						</div>
						{#if liveUpdateOp.progress >= 0}
							<ProgressBar value={liveUpdateOp.progress} max={100} />
						{/if}
					</div>
				{/if}

				{#if showUpdateLog && updateLog.length > 0}
					<div class="terminal-panel mb-4">
						{#each updateLog as line}
							<div class="terminal-line">{line}</div>
						{/each}
					</div>
				{/if}

				<div class="flex flex-wrap gap-3">
					{#if nodeUpdateStatus && nodeUpdateStatus.pending_count > 0}
						{#if nodeUpdateStatus.security_count > 0}
							<Button size="sm" variant="primary" onclick={() => applyUpdates(true)}
								disabled={!!actionLoading} loading={actionLoading === 'security_upgrade'}>
								Apply Security Updates
							</Button>
						{/if}
						<Button size="sm" onclick={() => applyUpdates(false)}
							disabled={!!actionLoading} loading={actionLoading === 'full_upgrade'}>
							Apply All Updates
						</Button>
					{/if}
					<Button size="sm" onclick={() => triggerMaintenance('apt_update')} disabled={!!actionLoading} loading={actionLoading === 'apt_update'}>
						apt update
					</Button>
					{#if nodeUpdateStatus?.reboot_required}
						<Button size="sm" variant="danger" onclick={rebootNode} disabled={!!actionLoading} loading={actionLoading === 'reboot'}>
							Reboot
						</Button>
					{:else}
						<Button size="sm" variant="danger" onclick={() => triggerMaintenance('reboot')} disabled={!!actionLoading} loading={actionLoading === 'reboot'}>
							Reboot
						</Button>
					{/if}
					{#if updateLog.length > 0}
						<Button size="sm" variant="ghost" onclick={() => { showUpdateLog = !showUpdateLog; }}>
							{showUpdateLog ? 'Hide Log' : 'Show Log'}
						</Button>
					{/if}
				</div>
			</Panel>
		{/if}

		<!-- Running Containers -->
		{#if containers.length > 0}
			<Panel title="Running Containers" class="mb-6">
				<div style="overflow-x: auto;">
					<table class="hive-table" style="min-width: 500px;">
						<thead>
							<tr>
								<th class="text-left">Container</th>
								<th class="text-left">Image</th>
								<th class="text-right">CPU %</th>
								<th class="text-right">Memory</th>
							</tr>
						</thead>
						<tbody>
							{#each containers as c}
								<tr>
									<td class="font-mono text-xs truncate max-w-[200px]">{c.name}</td>
									<td class="text-xs truncate max-w-[200px]" style="color: var(--color-text-muted);">{c.image}</td>
									<td class="text-right tabular text-xs">{(c.cpuPct ?? 0).toFixed(1)}%</td>
									<td class="text-right tabular text-xs">{formatBytes(c.memBytes ?? 0)}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</Panel>
		{/if}
	{:else}
		<Panel class="text-center py-12">
			<p style="color: var(--color-text-muted);">No metrics available for this node</p>
			<p class="text-xs mt-2" style="color: var(--color-text-muted);">Prometheus may still be collecting data.</p>
			<a href="/nodes" class="text-sm hover:underline mt-4 inline-block" style="color: var(--color-primary);">Back to Nodes</a>
		</Panel>
	{/if}
</div>

<style>
	.gauge-card {
		border-radius: var(--radius-lg);
		padding: var(--space-lg);
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 0.5rem;
		background-color: var(--color-surface);
		border: 1px solid var(--color-border);
	}
	.gauge-sub {
		font-size: 0.6875rem;
		color: var(--color-text-muted);
		text-align: center;
		font-variant-numeric: tabular-nums;
	}
	.detail-label {
		font-size: 0.625rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		margin-bottom: var(--space-xs);
		color: var(--color-text-muted);
	}
	.tabular { font-variant-numeric: tabular-nums; }
	.range-btn {
		padding: 0.25rem 0.625rem;
		border-radius: var(--radius-sm);
		font-size: 0.75rem;
		border: 1px solid var(--color-border);
		background-color: var(--color-surface);
		color: var(--color-text-muted);
		cursor: pointer;
		transition: all 0.15s ease;
	}
	.range-btn.active {
		background-color: var(--color-primary);
		color: var(--color-bg);
		border-color: var(--color-primary);
	}
	.range-btn:hover:not(.active) {
		border-color: var(--color-primary-border);
	}
	.pkg-list-header, .pkg-list-row {
		display: grid;
		grid-template-columns: 2fr 1fr 1fr 0.7fr;
		padding: 0.25rem 0.5rem;
		gap: 0.5rem;
	}
	.pkg-list-header {
		font-weight: 600;
		color: var(--color-text-muted);
		border-bottom: 1px solid var(--color-border);
		font-size: 0.75rem;
	}
	.pkg-list-row {
		border-bottom: 1px solid rgba(255,255,255,0.03);
	}
	.security-row {
		background: rgba(239,68,68,0.05);
	}
	.update-op-bar {
		padding: 0.5rem 0.75rem;
		background: rgba(234,179,8,0.05);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
	}
	.terminal-panel {
		background: #0a0a0a;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		padding: 0.5rem;
		max-height: 300px;
		overflow-y: auto;
		font-family: var(--font-mono, monospace);
		font-size: 0.7rem;
		line-height: 1.4;
	}
	.terminal-line {
		color: var(--color-text-muted);
		white-space: pre-wrap;
		word-break: break-all;
	}
	@media (max-width: 768px) {
		.gauge-card {
			padding: 0.75rem;
		}
		.detail-label {
			font-size: 0.75rem;
		}
		.range-btn {
			min-height: 36px;
			padding: 0.375rem 0.75rem;
		}
	}
</style>
