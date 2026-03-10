<script lang="ts">
	import { api } from '$lib/api';
	import type { NodeUpdateStatus, ServiceUpdateStatus, UpdateEvent, UpdatesSummary, UpdatePolicy } from '$lib/types';
	import { updatesStore } from '$lib/stores/updates.svelte';
	import { onMount } from 'svelte';
	import { invalidateAll } from '$app/navigation';

	let { data } = $props();
	let summary = $state<UpdatesSummary>(data.summary);
	let nodeStatuses = $state<NodeUpdateStatus[]>(data.nodeStatuses ?? []);
	let serviceStatuses = $state<ServiceUpdateStatus[]>(data.serviceStatuses ?? []);
	let history = $state<UpdateEvent[]>(data.history ?? []);
	let policies = $state<UpdatePolicy[]>(data.policies ?? []);
	let activeTab = $state<'nodes' | 'services' | 'history' | 'policies'>('nodes');
	let error = $state('');
	let success = $state('');
	let loading = $state<Record<string, boolean>>({});
	let expandedNodes = $state<Set<string>>(new Set());

	let showNewPolicy = $state(false);
	let newPolicy = $state({
		target_type: 'global' as string,
		target_id: '',
		auto_update: false,
		auto_restart: false,
		maintenance_window_start: '',
		maintenance_window_end: '',
		maintenance_window_days: '',
		security_only: false,
		pre_update_backup: true,
		notify_on_update: true
	});

	let globalPolicy = $derived(policies.find(p => p.target_type === 'global'));
	let nodePolicies = $derived(policies.filter(p => p.target_type === 'node'));
	let appPolicies = $derived(policies.filter(p => p.target_type === 'app'));

	const daysOptions = [
		{ value: 'mon', label: 'Mon' },
		{ value: 'tue', label: 'Tue' },
		{ value: 'wed', label: 'Wed' },
		{ value: 'thu', label: 'Thu' },
		{ value: 'fri', label: 'Fri' },
		{ value: 'sat', label: 'Sat' },
		{ value: 'sun', label: 'Sun' },
	];

	onMount(() => {
		updatesStore.subscribe();
		updatesStore.seedNodeStatuses(nodeStatuses);
		updatesStore.seedServiceStatuses(serviceStatuses);
		return () => updatesStore.unsubscribe();
	});

	let liveNodeStatuses = $derived(
		nodeStatuses.map(n => {
			const live = updatesStore.state.nodeStatuses.get(n.node_id);
			return live || n;
		})
	);

	let activeOps = $derived(updatesStore.state.activeNodeOperations);

	async function refreshSummary() {
		try {
			summary = await api.updatesSummary();
		} catch {}
	}

	async function checkAllNodes() {
		loading = { ...loading, checkAll: true };
		error = '';
		try {
			await api.checkAllNodeUpdates();
			setTimeout(async () => {
				await invalidateAll();
				await refreshSummary();
				loading = { ...loading, checkAll: false };
			}, 5000);
		} catch (e: any) {
			error = e.message;
			loading = { ...loading, checkAll: false };
		}
	}

	async function checkNode(nodeId: string) {
		loading = { ...loading, [`check-${nodeId}`]: true };
		error = '';
		try {
			const result = await api.checkNodeUpdates(nodeId);
			const idx = nodeStatuses.findIndex(n => n.node_id === nodeId);
			if (idx >= 0 && result) {
				nodeStatuses[idx] = { ...nodeStatuses[idx], ...result };
				nodeStatuses = [...nodeStatuses];
			}
			await refreshSummary();
		} catch (e: any) {
			error = e.message;
		}
		loading = { ...loading, [`check-${nodeId}`]: false };
	}

	async function applyNodeUpdates(nodeId: string, securityOnly = false) {
		loading = { ...loading, [`apply-${nodeId}`]: true };
		error = '';
		updatesStore.clearNodeLog(nodeId);
		try {
			await api.applyNodeUpdates(nodeId, { security_only: securityOnly });
		} catch (e: any) {
			error = e.message;
		}
		loading = { ...loading, [`apply-${nodeId}`]: false };
	}

	async function rebootNode(nodeId: string) {
		if (!confirm(`Reboot node ${nodeId}? This will temporarily take it offline.`)) return;
		loading = { ...loading, [`reboot-${nodeId}`]: true };
		try {
			await api.applyNodeUpdates(nodeId, { action: 'reboot' });
		} catch (e: any) {
			error = e.message;
		}
		loading = { ...loading, [`reboot-${nodeId}`]: false };
	}

	async function applyServiceUpdate(serviceName: string) {
		loading = { ...loading, [`svc-${serviceName}`]: true };
		error = '';
		try {
			await api.applyServiceUpdate(serviceName);
			serviceStatuses = serviceStatuses.map(s =>
				s.service_name === serviceName ? { ...s, update_available: false } : s
			);
			await refreshSummary();
		} catch (e: any) {
			error = e.message;
		}
		loading = { ...loading, [`svc-${serviceName}`]: false };
	}

	async function applyAllServiceUpdates() {
		loading = { ...loading, applyAllSvc: true };
		error = '';
		try {
			await api.applyAllServiceUpdates();
			await invalidateAll();
			await refreshSummary();
		} catch (e: any) {
			error = e.message;
		}
		loading = { ...loading, applyAllSvc: false };
	}

	async function loadHistory() {
		try {
			history = await api.updatesHistory({ limit: 50 });
		} catch {}
	}

	function toggleNodeExpand(nodeId: string) {
		const next = new Set(expandedNodes);
		if (next.has(nodeId)) next.delete(nodeId);
		else next.add(nodeId);
		expandedNodes = next;
	}

	function statusColor(status: string) {
		switch (status) {
			case 'success': return 'var(--color-success)';
			case 'failed': return 'var(--color-danger)';
			case 'running': return 'var(--color-warning)';
			case 'rolled_back': return 'var(--color-info)';
			default: return 'var(--color-text-muted)';
		}
	}

	function timeAgo(dateStr: string) {
		const diff = Date.now() - new Date(dateStr).getTime();
		const mins = Math.floor(diff / 60000);
		if (mins < 1) return 'just now';
		if (mins < 60) return `${mins}m ago`;
		const hours = Math.floor(mins / 60);
		if (hours < 24) return `${hours}h ago`;
		return `${Math.floor(hours / 24)}d ago`;
	}

	async function createPolicy() {
		loading = { ...loading, create: true };
		error = '';
		try {
			const created = await api.createUpdatePolicy(newPolicy);
			policies = [...policies, created];
			showNewPolicy = false;
			newPolicy = { target_type: 'global', target_id: '', auto_update: false, auto_restart: false, maintenance_window_start: '', maintenance_window_end: '', maintenance_window_days: '', security_only: false, pre_update_backup: true, notify_on_update: true };
			success = 'Policy created successfully';
			setTimeout(() => success = '', 3000);
		} catch (e: any) {
			error = e.message;
		}
		loading = { ...loading, create: false };
	}

	async function toggleAutoUpdate(policy: UpdatePolicy) {
		loading = { ...loading, [`toggle-${policy.id}`]: true };
		try {
			const updated = await api.updateUpdatePolicy(policy.id, {
				...policy,
				auto_update: !policy.auto_update
			});
			policies = policies.map(p => p.id === policy.id ? updated : p);
		} catch (e: any) {
			error = e.message;
		}
		loading = { ...loading, [`toggle-${policy.id}`]: false };
	}

	async function updatePolicyField(policy: UpdatePolicy, field: string, value: any) {
		try {
			const updated = await api.updateUpdatePolicy(policy.id, {
				...policy,
				[field]: value
			});
			policies = policies.map(p => p.id === policy.id ? updated : p);
		} catch (e: any) {
			error = e.message;
		}
	}

	async function deletePolicy(id: string) {
		if (!confirm('Delete this update policy?')) return;
		loading = { ...loading, [`delete-${id}`]: true };
		try {
			await api.deleteUpdatePolicy(id);
			policies = policies.filter(p => p.id !== id);
			success = 'Policy deleted';
			setTimeout(() => success = '', 3000);
		} catch (e: any) {
			error = e.message;
		}
		loading = { ...loading, [`delete-${id}`]: false };
	}
</script>

<div class="page-header">
	<div>
		<h1 class="page-title">Updates</h1>
		<p class="page-subtitle">Manage OS and service updates across your cluster</p>
	</div>
	<div class="header-actions">
		<button class="btn btn-secondary" onclick={checkAllNodes} disabled={loading.checkAll}>
			{loading.checkAll ? 'Checking...' : 'Check All Nodes'}
		</button>
	</div>
</div>

{#if error}
	<div class="alert alert-danger">{error}</div>
{/if}
{#if success}
	<div class="alert alert-success">{success}</div>
{/if}

<div class="summary-grid">
	<div class="summary-card">
		<div class="summary-value">{summary.pending_updates}</div>
		<div class="summary-label">Pending OS Updates</div>
	</div>
	<div class="summary-card summary-security">
		<div class="summary-value">{summary.security_updates}</div>
		<div class="summary-label">Security Updates</div>
	</div>
	<div class="summary-card">
		<div class="summary-value">{summary.service_updates}</div>
		<div class="summary-label">Service Updates</div>
	</div>
	<div class="summary-card" class:summary-reboot={summary.reboot_required > 0}>
		<div class="summary-value">{summary.reboot_required}</div>
		<div class="summary-label">Reboot Required</div>
	</div>
</div>

{#if updatesStore.state.connected}
	<div class="live-indicator">
		<span class="live-dot"></span> Live
	</div>
{/if}

<div class="tabs">
	<button class="tab" class:active={activeTab === 'nodes'} onclick={() => activeTab = 'nodes'}>
		Node Updates
		{#if summary.pending_updates > 0}
			<span class="tab-badge">{summary.pending_updates}</span>
		{/if}
	</button>
	<button class="tab" class:active={activeTab === 'services'} onclick={() => activeTab = 'services'}>
		Service Updates
		{#if summary.service_updates > 0}
			<span class="tab-badge">{summary.service_updates}</span>
		{/if}
	</button>
	<button class="tab" class:active={activeTab === 'history'} onclick={() => { activeTab = 'history'; loadHistory(); }}>
		History
	</button>
	<button class="tab" class:active={activeTab === 'policies'} onclick={() => activeTab = 'policies'}>
		Policies
	</button>
</div>

<div class="tab-content">
	{#if activeTab === 'nodes'}
		{#if liveNodeStatuses.length === 0}
			<div class="empty-state">
				<p>No node update data yet. Click "Check All Nodes" to scan for updates.</p>
			</div>
		{:else}
			<div class="node-list">
				{#each liveNodeStatuses as node}
					{@const op = activeOps.get(node.node_id)}
					{@const log = updatesStore.state.nodeOutputLog.get(node.node_id)}
					<div class="node-card" class:has-updates={node.pending_count > 0} class:has-security={node.security_count > 0}>
						<div class="node-header" onclick={() => toggleNodeExpand(node.node_id)}>
							<div class="node-info">
								<div class="node-name">
									{#if node.pending_count === 0}
										<span class="status-dot green"></span>
									{:else if node.security_count > 0}
										<span class="status-dot red"></span>
									{:else}
										<span class="status-dot amber"></span>
									{/if}
									{node.hostname}
								</div>
								<div class="node-meta">
									{node.os_info || 'Linux'} &middot; Kernel {node.kernel_version || 'unknown'}
								</div>
							</div>
							<div class="node-stats">
								{#if node.reboot_required}
									<span class="badge badge-reboot">Reboot Required</span>
								{/if}
								{#if node.security_count > 0}
									<span class="badge badge-security">{node.security_count} security</span>
								{/if}
								{#if node.pending_count > 0}
									<span class="badge badge-pending">{node.pending_count} updates</span>
								{:else}
									<span class="badge badge-uptodate">Up to date</span>
								{/if}
								<span class="expand-icon">{expandedNodes.has(node.node_id) ? '▾' : '▸'}</span>
							</div>
						</div>

						{#if op}
							<div class="operation-bar">
								<div class="op-info">
									<span class="op-action">{op.action}</span>
									<span class="op-status" style="color: {statusColor(op.status)}">{op.status}</span>
								</div>
								{#if op.progress >= 0}
									<div class="progress-bar">
										<div class="progress-fill" style="width: {op.progress}%"></div>
									</div>
								{/if}
							</div>
						{/if}

						{#if expandedNodes.has(node.node_id)}
							<div class="node-details">
								<div class="detail-row">
									<span class="detail-label">Package Manager</span>
									<span class="detail-value">{node.package_manager || 'unknown'}</span>
								</div>
								<div class="detail-row">
									<span class="detail-label">Last Checked</span>
									<span class="detail-value">{node.last_checked_at ? timeAgo(node.last_checked_at) : 'never'}</span>
								</div>

								{#if node.pending_packages && node.pending_packages.length > 0}
									<div class="packages-table">
										<div class="pkg-header">
											<span>Package</span>
											<span>Current</span>
											<span>Available</span>
											<span>Type</span>
										</div>
										{#each node.pending_packages as pkg}
											<div class="pkg-row" class:security-pkg={pkg.is_security}>
												<span class="pkg-name">{pkg.name}</span>
												<span class="pkg-version">{pkg.current_version}</span>
												<span class="pkg-version pkg-new">{pkg.new_version}</span>
												<span>{pkg.is_security ? '🔒 Security' : 'Standard'}</span>
											</div>
										{/each}
									</div>
								{/if}

								{#if log && log.length > 0}
									<div class="terminal-output">
										{#each log as line}
											<div class="terminal-line">{line}</div>
										{/each}
									</div>
								{/if}

								<div class="node-actions">
									<button class="btn btn-secondary btn-sm" onclick={() => checkNode(node.node_id)} disabled={loading[`check-${node.node_id}`]}>
										{loading[`check-${node.node_id}`] ? 'Checking...' : 'Check Now'}
									</button>
									{#if node.pending_count > 0}
										{#if node.security_count > 0}
											<button class="btn btn-primary btn-sm" onclick={() => applyNodeUpdates(node.node_id, true)} disabled={loading[`apply-${node.node_id}`]}>
												Apply Security Only
											</button>
										{/if}
										<button class="btn btn-primary btn-sm" onclick={() => applyNodeUpdates(node.node_id)} disabled={loading[`apply-${node.node_id}`]}>
											{loading[`apply-${node.node_id}`] ? 'Applying...' : 'Apply All Updates'}
										</button>
									{/if}
									{#if node.reboot_required}
										<button class="btn btn-danger btn-sm" onclick={() => rebootNode(node.node_id)} disabled={loading[`reboot-${node.node_id}`]}>
											Reboot
										</button>
									{/if}
								</div>
							</div>
						{/if}
					</div>
				{/each}
			</div>
		{/if}

	{:else if activeTab === 'services'}
		{@const updatable = serviceStatuses.filter(s => s.update_available)}
		{@const current = serviceStatuses.filter(s => !s.update_available)}

		{#if serviceStatuses.length === 0}
			<div class="empty-state">
				<p>No service update data yet. Image checks run automatically every 4 hours.</p>
			</div>
		{:else}
			{#if updatable.length > 0}
				<div class="section-header">
					<h3>Updates Available ({updatable.length})</h3>
					<button class="btn btn-primary btn-sm" onclick={applyAllServiceUpdates} disabled={loading.applyAllSvc}>
						{loading.applyAllSvc ? 'Updating...' : 'Update All'}
					</button>
				</div>
				<div class="service-table">
					<div class="svc-header">
						<span>Service</span>
						<span>Current Image</span>
						<span>Available</span>
						<span>Last Checked</span>
						<span>Actions</span>
					</div>
					{#each updatable as svc}
						{@const svcOp = updatesStore.state.activeServiceOperations.get(svc.service_name)}
						<div class="svc-row">
							<span class="svc-name">{svc.service_name.replace('hive-app-', '')}</span>
							<span class="svc-image">{svc.current_image}</span>
							<span class="svc-new">{svc.latest_version || 'newer digest'}</span>
							<span class="svc-time">{svc.last_checked_at ? timeAgo(svc.last_checked_at) : '-'}</span>
							<span class="svc-actions">
								{#if svcOp}
									<span class="badge" style="background: {statusColor(svcOp.status)}33; color: {statusColor(svcOp.status)}">{svcOp.status}</span>
								{:else}
									<button class="btn btn-primary btn-xs" onclick={() => applyServiceUpdate(svc.service_name)} disabled={loading[`svc-${svc.service_name}`]}>
										Update
									</button>
								{/if}
							</span>
						</div>
					{/each}
				</div>
			{/if}

			{#if current.length > 0}
				<div class="section-header" style="margin-top: var(--space-lg)">
					<h3>Up to Date ({current.length})</h3>
				</div>
				<div class="service-table">
					<div class="svc-header">
						<span>Service</span>
						<span>Image</span>
						<span>Status</span>
						<span>Last Checked</span>
					</div>
					{#each current as svc}
						<div class="svc-row current">
							<span class="svc-name">{svc.service_name.replace('hive-app-', '')}</span>
							<span class="svc-image">{svc.current_image}</span>
							<span><span class="badge badge-uptodate">Current</span></span>
							<span class="svc-time">{svc.last_checked_at ? timeAgo(svc.last_checked_at) : '-'}</span>
						</div>
					{/each}
				</div>
			{/if}
		{/if}

	{:else if activeTab === 'history'}
		{#if history.length === 0}
			<div class="empty-state">
				<p>No update events recorded yet.</p>
			</div>
		{:else}
			<div class="history-list">
				{#each history as event}
					<div class="history-item">
						<div class="history-icon" style="background: {statusColor(event.status)}22; color: {statusColor(event.status)}">
							{#if event.event_type === 'node_os'}
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="2" width="20" height="8" rx="2"/><rect x="2" y="14" width="20" height="8" rx="2"/></svg>
							{:else if event.event_type === 'service_image'}
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/></svg>
							{:else}
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3"/><path d="M12 1v6m0 6v6m-9-9h6m6 0h6"/></svg>
							{/if}
						</div>
						<div class="history-content">
							<div class="history-title">
								<span class="history-target">{event.target_name || event.target_id}</span>
								<span class="history-type">{event.event_type.replace('_', ' ')}</span>
								<span class="badge" style="background: {statusColor(event.status)}22; color: {statusColor(event.status)}">{event.status}</span>
							</div>
							<div class="history-meta">
								{#if event.previous_version && event.new_version}
									<span class="version-change">{event.previous_version.slice(0, 12)} → {event.new_version.slice(0, 12)}</span>
								{/if}
								<span>Triggered by {event.triggered_by}</span>
								<span>{timeAgo(event.started_at)}</span>
							</div>
							{#if event.details}
								<div class="history-details">{event.details}</div>
							{/if}
						</div>
					</div>
				{/each}
			</div>
		{/if}

	{:else if activeTab === 'policies'}
		<div class="policies-header">
			<p class="section-desc">Configure automatic updates, maintenance windows, and per-node/per-service policies.</p>
			<button class="btn btn-primary btn-sm" onclick={() => showNewPolicy = true}>New Policy</button>
		</div>

		<!-- Global Defaults -->
		<div class="policy-section">
			<h3 class="policy-section-title">Global Defaults</h3>
			{#if globalPolicy}
				<div class="policy-card">
					<div class="policy-grid">
						<label class="toggle-label">
							<input type="checkbox" checked={globalPolicy.auto_update}
								onchange={() => toggleAutoUpdate(globalPolicy)} />
							<span>Auto-update enabled</span>
						</label>
						<label class="toggle-label">
							<input type="checkbox" checked={globalPolicy.auto_restart}
								onchange={() => updatePolicyField(globalPolicy, 'auto_restart', !globalPolicy.auto_restart)} />
							<span>Auto-restart after updates</span>
						</label>
						<label class="toggle-label">
							<input type="checkbox" checked={globalPolicy.security_only}
								onchange={() => updatePolicyField(globalPolicy, 'security_only', !globalPolicy.security_only)} />
							<span>Security updates only</span>
						</label>
						<label class="toggle-label">
							<input type="checkbox" checked={globalPolicy.pre_update_backup}
								onchange={() => updatePolicyField(globalPolicy, 'pre_update_backup', !globalPolicy.pre_update_backup)} />
							<span>Pre-update database backup</span>
						</label>
						<label class="toggle-label">
							<input type="checkbox" checked={globalPolicy.notify_on_update}
								onchange={() => updatePolicyField(globalPolicy, 'notify_on_update', !globalPolicy.notify_on_update)} />
							<span>Send notifications</span>
						</label>
					</div>
					<div class="maintenance-window">
						<p class="field-label">Maintenance Window</p>
						<div class="window-inputs">
							<input type="time" value={globalPolicy.maintenance_window_start}
								onchange={(e) => updatePolicyField(globalPolicy, 'maintenance_window_start', e.currentTarget.value)} />
							<span>to</span>
							<input type="time" value={globalPolicy.maintenance_window_end}
								onchange={(e) => updatePolicyField(globalPolicy, 'maintenance_window_end', e.currentTarget.value)} />
						</div>
						<div class="day-toggles">
							{#each daysOptions as day}
								<button
									class="day-btn"
									class:active={globalPolicy.maintenance_window_days.includes(day.value)}
									onclick={() => {
										const days = globalPolicy.maintenance_window_days.split(',').filter(d => d);
										const idx = days.indexOf(day.value);
										if (idx >= 0) days.splice(idx, 1);
										else days.push(day.value);
										updatePolicyField(globalPolicy, 'maintenance_window_days', days.join(','));
									}}
								>
									{day.label}
								</button>
							{/each}
						</div>
					</div>
				</div>
			{:else}
				<div class="empty-state" style="padding: var(--space-lg);">
					<p>No global policy configured.</p>
					<button class="btn btn-primary btn-sm" style="margin-top: var(--space-sm);" onclick={() => { newPolicy.target_type = 'global'; showNewPolicy = true; }}>
						Create Global Policy
					</button>
				</div>
			{/if}
		</div>

		<!-- Node Policies -->
		<div class="policy-section">
			<h3 class="policy-section-title">Node OS Update Policies</h3>
			{#if nodePolicies.length > 0}
				<div class="policy-list">
					{#each nodePolicies as policy}
						<div class="policy-card compact">
							<div class="policy-row">
								<span class="policy-target">{policy.target_id}</span>
								<div class="policy-actions">
									<label class="toggle-label">
										<input type="checkbox" checked={policy.auto_update}
											onchange={() => toggleAutoUpdate(policy)} />
										<span class="text-xs">Auto-update</span>
									</label>
									<label class="toggle-label">
										<input type="checkbox" checked={policy.security_only}
											onchange={() => updatePolicyField(policy, 'security_only', !policy.security_only)} />
										<span class="text-xs">Security only</span>
									</label>
									<button class="btn btn-danger btn-xs" onclick={() => deletePolicy(policy.id)}>Remove</button>
								</div>
							</div>
							{#if policy.maintenance_window_start}
								<div class="text-xs" style="color: var(--color-text-muted); margin-top: var(--space-xs);">
									Window: {policy.maintenance_window_start} - {policy.maintenance_window_end}
									{#if policy.maintenance_window_days} ({policy.maintenance_window_days}){/if}
								</div>
							{/if}
						</div>
					{/each}
				</div>
			{:else}
				<div class="empty-state" style="padding: var(--space-md);">
					<p>No per-node policies. Nodes use the global policy by default.</p>
				</div>
			{/if}
		</div>

		<!-- Service Policies -->
		<div class="policy-section">
			<h3 class="policy-section-title">Service Image Update Policies</h3>
			{#if appPolicies.length > 0}
				<div class="policy-list">
					{#each appPolicies as policy}
						<div class="policy-card compact">
							<div class="policy-row">
								<span class="policy-target">{policy.target_id}</span>
								<div class="policy-actions">
									<label class="toggle-label">
										<input type="checkbox" checked={policy.auto_update}
											onchange={() => toggleAutoUpdate(policy)} />
										<span class="text-xs">Auto-update</span>
									</label>
									<label class="toggle-label">
										<input type="checkbox" checked={policy.pre_update_backup}
											onchange={() => updatePolicyField(policy, 'pre_update_backup', !policy.pre_update_backup)} />
										<span class="text-xs">Pre-backup</span>
									</label>
									<button class="btn btn-danger btn-xs" onclick={() => deletePolicy(policy.id)}>Remove</button>
								</div>
							</div>
						</div>
					{/each}
				</div>
			{:else}
				<div class="empty-state" style="padding: var(--space-md);">
					<p>No per-service policies. Services use the global policy by default.</p>
				</div>
			{/if}
		</div>

		<!-- Schedule Info -->
		<div class="policy-section">
			<h3 class="policy-section-title">Automatic Checks</h3>
			<div class="info-grid">
				<div class="info-item">
					<span class="field-label">Image Check</span>
					<span>Every 4 hours</span>
				</div>
				<div class="info-item">
					<span class="field-label">Services Tracked</span>
					<span>{serviceStatuses.length}</span>
				</div>
				<div class="info-item">
					<span class="field-label">Git Polling</span>
					<span>Every 15 min</span>
				</div>
			</div>
		</div>

		<!-- New Policy Modal -->
		{#if showNewPolicy}
			<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
			<div class="modal-backdrop" onclick={() => showNewPolicy = false}>
				<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
				<div class="modal" onclick={(e) => e.stopPropagation()}>
					<h3 class="modal-title">Create Update Policy</h3>
					<div class="form-group">
						<label for="pol-type" class="field-label">Target Type</label>
						<select id="pol-type" bind:value={newPolicy.target_type} class="form-input">
							<option value="global">Global</option>
							<option value="node">Node</option>
							<option value="app">Application</option>
						</select>
					</div>
					{#if newPolicy.target_type === 'node'}
						<div class="form-group">
							<label for="pol-node" class="field-label">Node</label>
							<select id="pol-node" bind:value={newPolicy.target_id} class="form-input">
								<option value="">Select a node...</option>
								{#each nodeStatuses as n}
									<option value={n.node_id}>{n.hostname}</option>
								{/each}
							</select>
						</div>
					{:else if newPolicy.target_type === 'app'}
						<div class="form-group">
							<label for="pol-svc" class="field-label">Service Name</label>
							<select id="pol-svc" bind:value={newPolicy.target_id} class="form-input">
								<option value="">Select a service...</option>
								{#each serviceStatuses as s}
									<option value={s.service_name}>{s.service_name.replace('hive-app-', '')}</option>
								{/each}
							</select>
						</div>
					{/if}
					<div class="form-group">
						<label class="toggle-label"><input type="checkbox" bind:checked={newPolicy.auto_update} /> <span>Enable auto-updates</span></label>
					</div>
					<div class="form-group">
						<label class="toggle-label"><input type="checkbox" bind:checked={newPolicy.security_only} /> <span>Security updates only</span></label>
					</div>
					<div class="form-group">
						<label class="toggle-label"><input type="checkbox" bind:checked={newPolicy.pre_update_backup} /> <span>Pre-update database backup</span></label>
					</div>
					<div class="form-group">
						<label class="toggle-label"><input type="checkbox" bind:checked={newPolicy.auto_restart} /> <span>Auto-restart after updates</span></label>
					</div>
					<div class="form-group">
						<label for="pol-win-start" class="field-label">Maintenance Window</label>
						<div class="window-inputs">
							<input id="pol-win-start" type="time" bind:value={newPolicy.maintenance_window_start} class="form-input" />
							<span>to</span>
							<input type="time" bind:value={newPolicy.maintenance_window_end} class="form-input" />
						</div>
					</div>
					<div class="modal-actions">
						<button class="btn btn-secondary" onclick={() => showNewPolicy = false}>Cancel</button>
						<button class="btn btn-primary" onclick={createPolicy} disabled={loading.create}>
							{loading.create ? 'Creating...' : 'Create Policy'}
						</button>
					</div>
				</div>
			</div>
		{/if}
	{/if}
</div>

<style>
	.page-subtitle {
		color: var(--color-text-muted);
		font-size: 0.875rem;
		margin-top: var(--space-xs);
	}
	.header-actions {
		display: flex;
		gap: var(--space-sm);
	}

	.summary-grid {
		display: grid;
		grid-template-columns: repeat(4, 1fr);
		gap: var(--space-md);
		margin-bottom: var(--space-lg);
	}
	.summary-card {
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		padding: var(--space-lg);
		text-align: center;
	}
	.summary-value {
		font-size: 2rem;
		font-weight: 700;
		color: var(--color-text);
		line-height: 1;
	}
	.summary-label {
		font-size: 0.8rem;
		color: var(--color-text-muted);
		margin-top: var(--space-xs);
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}
	.summary-security .summary-value { color: var(--color-danger); }
	.summary-reboot .summary-value { color: var(--color-info); }

	.live-indicator {
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: 0.75rem;
		color: var(--color-success);
		margin-bottom: var(--space-sm);
	}
	.live-dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--color-success);
		animation: pulse-dot 2s infinite;
	}
	@keyframes pulse-dot {
		0%, 100% { opacity: 1; }
		50% { opacity: 0.4; }
	}

	.tabs {
		display: flex;
		gap: var(--space-xs);
		border-bottom: 1px solid var(--color-border);
		margin-bottom: var(--space-lg);
	}
	.tab {
		padding: var(--space-sm) var(--space-md);
		background: none;
		border: none;
		color: var(--color-text-muted);
		cursor: pointer;
		font-size: 0.875rem;
		border-bottom: 2px solid transparent;
		display: flex;
		align-items: center;
		gap: var(--space-xs);
		transition: all var(--transition-fast);
	}
	.tab:hover { color: var(--color-text); }
	.tab.active {
		color: var(--color-primary);
		border-bottom-color: var(--color-primary);
	}
	.tab-badge {
		background: var(--color-primary);
		color: #000;
		padding: 1px 6px;
		border-radius: var(--radius-full);
		font-size: 0.7rem;
		font-weight: 600;
	}

	.node-list { display: flex; flex-direction: column; gap: var(--space-sm); }
	.node-card {
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		overflow: hidden;
		transition: border-color var(--transition-fast);
	}
	.node-card.has-security { border-left: 3px solid var(--color-danger); }
	.node-card.has-updates:not(.has-security) { border-left: 3px solid var(--color-warning); }

	.node-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: var(--space-md) var(--space-lg);
		cursor: pointer;
	}
	.node-header:hover { background: var(--color-surface-hover); }
	.node-name {
		font-weight: 600;
		display: flex;
		align-items: center;
		gap: var(--space-sm);
	}
	.node-meta {
		font-size: 0.8rem;
		color: var(--color-text-muted);
		margin-top: 2px;
	}
	.node-stats {
		display: flex;
		align-items: center;
		gap: var(--space-sm);
	}

	.status-dot {
		width: 10px;
		height: 10px;
		border-radius: 50%;
		display: inline-block;
	}
	.status-dot.green { background: var(--color-success); }
	.status-dot.amber { background: var(--color-warning); }
	.status-dot.red { background: var(--color-danger); }

	.badge {
		padding: 2px 8px;
		border-radius: var(--radius-full);
		font-size: 0.75rem;
		font-weight: 500;
	}
	.badge-uptodate { background: var(--color-success-bg, rgba(34,197,94,0.1)); color: var(--color-success); }
	.badge-pending { background: rgba(234,179,8,0.1); color: var(--color-warning); }
	.badge-security { background: rgba(239,68,68,0.1); color: var(--color-danger); }
	.badge-reboot { background: rgba(59,130,246,0.15); color: var(--color-info); animation: pulse-dot 2s infinite; }

	.expand-icon { color: var(--color-text-muted); font-size: 0.8rem; }

	.operation-bar {
		padding: var(--space-sm) var(--space-lg);
		background: rgba(234,179,8,0.05);
		border-top: 1px solid var(--color-border);
	}
	.op-info {
		display: flex;
		justify-content: space-between;
		font-size: 0.8rem;
		margin-bottom: var(--space-xs);
	}
	.op-action { font-weight: 600; }
	.progress-bar {
		height: 4px;
		background: var(--color-border);
		border-radius: 2px;
		overflow: hidden;
	}
	.progress-fill {
		height: 100%;
		background: var(--color-primary);
		transition: width 0.3s ease;
		border-radius: 2px;
	}

	.node-details {
		padding: var(--space-md) var(--space-lg);
		border-top: 1px solid var(--color-border);
		background: rgba(0,0,0,0.15);
	}
	.detail-row {
		display: flex;
		justify-content: space-between;
		padding: var(--space-xs) 0;
		font-size: 0.85rem;
	}
	.detail-label { color: var(--color-text-muted); }

	.packages-table {
		margin: var(--space-md) 0;
		font-size: 0.8rem;
	}
	.pkg-header, .pkg-row {
		display: grid;
		grid-template-columns: 2fr 1fr 1fr 1fr;
		padding: var(--space-xs) var(--space-sm);
		gap: var(--space-sm);
	}
	.pkg-header {
		font-weight: 600;
		color: var(--color-text-muted);
		border-bottom: 1px solid var(--color-border);
	}
	.pkg-row { border-bottom: 1px solid rgba(255,255,255,0.03); }
	.pkg-row.security-pkg { background: rgba(239,68,68,0.05); }
	.pkg-name { font-family: var(--font-mono); }
	.pkg-version { font-family: var(--font-mono); color: var(--color-text-muted); }
	.pkg-new { color: var(--color-success); }

	.terminal-output {
		background: #0a0a0a;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		padding: var(--space-sm);
		margin: var(--space-md) 0;
		max-height: 300px;
		overflow-y: auto;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		line-height: 1.4;
	}
	.terminal-line { color: var(--color-text-muted); white-space: pre-wrap; word-break: break-all; }

	.node-actions {
		display: flex;
		gap: var(--space-sm);
		margin-top: var(--space-md);
	}

	.section-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: var(--space-md);
	}
	.section-header h3 {
		font-size: 1rem;
		font-weight: 600;
	}

	.service-table { font-size: 0.85rem; }
	.svc-header, .svc-row {
		display: grid;
		grid-template-columns: 1.5fr 2fr 1fr 1fr 1fr;
		padding: var(--space-sm) var(--space-md);
		gap: var(--space-sm);
		align-items: center;
	}
	.svc-header {
		font-weight: 600;
		color: var(--color-text-muted);
		border-bottom: 1px solid var(--color-border);
	}
	.svc-row {
		background: var(--color-surface);
		border-bottom: 1px solid var(--color-border);
	}
	.svc-row.current { opacity: 0.7; }
	.svc-name { font-weight: 500; }
	.svc-image { font-family: var(--font-mono); font-size: 0.8rem; color: var(--color-text-muted); overflow: hidden; text-overflow: ellipsis; }
	.svc-new { color: var(--color-success); font-family: var(--font-mono); }
	.svc-time { color: var(--color-text-muted); }
	.svc-actions { display: flex; justify-content: flex-end; }

	.btn-xs { padding: 2px 8px; font-size: 0.75rem; }
	.btn-sm { padding: 4px 12px; font-size: 0.8rem; }

	.history-list { display: flex; flex-direction: column; gap: var(--space-sm); }
	.history-item {
		display: flex;
		gap: var(--space-md);
		padding: var(--space-md);
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
	}
	.history-icon {
		width: 36px;
		height: 36px;
		border-radius: var(--radius-md);
		display: flex;
		align-items: center;
		justify-content: center;
		flex-shrink: 0;
	}
	.history-content { flex: 1; min-width: 0; }
	.history-title { display: flex; align-items: center; gap: var(--space-sm); flex-wrap: wrap; }
	.history-target { font-weight: 600; }
	.history-type { color: var(--color-text-muted); font-size: 0.8rem; }
	.history-meta {
		display: flex;
		gap: var(--space-md);
		font-size: 0.8rem;
		color: var(--color-text-muted);
		margin-top: var(--space-xs);
	}
	.version-change { font-family: var(--font-mono); }
	.history-details {
		font-size: 0.8rem;
		color: var(--color-text-muted);
		margin-top: var(--space-xs);
	}

	.alert {
		padding: var(--space-sm) var(--space-md);
		border-radius: var(--radius-md);
		margin-bottom: var(--space-md);
		font-size: 0.875rem;
	}
	.alert-danger {
		background: rgba(239,68,68,0.1);
		border: 1px solid rgba(239,68,68,0.3);
		color: var(--color-danger);
	}
	.alert-success {
		background: rgba(34,197,94,0.1);
		border: 1px solid rgba(34,197,94,0.3);
		color: var(--color-success);
	}

	/* Policies tab styles */
	.policies-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: var(--space-lg);
	}
	.policy-section {
		margin-bottom: var(--space-xl);
	}
	.policy-section-title {
		font-size: 1rem;
		font-weight: 600;
		margin-bottom: var(--space-sm);
	}
	.section-desc {
		color: var(--color-text-muted);
		font-size: 0.85rem;
	}
	.policy-card {
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		padding: var(--space-lg);
		margin-bottom: var(--space-sm);
	}
	.policy-card.compact { padding: var(--space-md); }
	.policy-grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
		gap: var(--space-md);
		margin-bottom: var(--space-md);
	}
	.policy-row {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: var(--space-md);
	}
	.policy-target { font-weight: 600; font-family: var(--font-mono); font-size: 0.9rem; }
	.policy-actions { display: flex; align-items: center; gap: var(--space-md); flex-wrap: wrap; }
	.policy-list { display: flex; flex-direction: column; gap: var(--space-sm); }
	.toggle-label {
		display: flex;
		align-items: center;
		gap: var(--space-sm);
		cursor: pointer;
		font-size: 0.85rem;
	}
	.toggle-label input[type="checkbox"] {
		accent-color: var(--color-primary);
		width: 16px;
		height: 16px;
	}
	.maintenance-window {
		border-top: 1px solid var(--color-border);
		padding-top: var(--space-md);
	}
	.window-inputs {
		display: flex;
		align-items: center;
		gap: var(--space-sm);
		margin: var(--space-sm) 0;
	}
	.window-inputs input[type="time"] {
		background: var(--color-bg);
		border: 1px solid var(--color-border);
		color: var(--color-text);
		padding: 4px 8px;
		border-radius: var(--radius-sm);
		font-size: 0.85rem;
	}
	.day-toggles { display: flex; gap: var(--space-xs); flex-wrap: wrap; }
	.day-btn {
		padding: 4px 10px;
		border-radius: var(--radius-sm);
		border: 1px solid var(--color-border);
		background: var(--color-surface);
		color: var(--color-text-muted);
		font-size: 0.75rem;
		cursor: pointer;
		transition: all var(--transition-fast);
	}
	.day-btn.active {
		background: var(--color-primary);
		color: #000;
		border-color: var(--color-primary);
	}
	.day-btn:hover:not(.active) { border-color: var(--color-primary); }
	.field-label {
		font-size: 0.75rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--color-text-muted);
		margin-bottom: var(--space-xs);
	}
	.info-grid {
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		gap: var(--space-md);
	}
	.info-item {
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		padding: var(--space-md);
		display: flex;
		flex-direction: column;
		gap: var(--space-xs);
	}
	.info-item span:last-child { font-size: 1.25rem; font-weight: 600; }
	.modal-backdrop {
		position: fixed;
		inset: 0;
		background: rgba(0,0,0,0.6);
		display: flex;
		align-items: center;
		justify-content: center;
		z-index: 50;
	}
	.modal {
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		padding: var(--space-xl);
		min-width: 400px;
		max-width: 500px;
	}
	.modal-title {
		font-size: 1.1rem;
		font-weight: 600;
		margin-bottom: var(--space-lg);
	}
	.form-group { margin-bottom: var(--space-md); }
	.form-input {
		width: 100%;
		padding: 6px 10px;
		background: var(--color-bg);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		color: var(--color-text);
		font-size: 0.85rem;
	}
	.modal-actions {
		display: flex;
		justify-content: flex-end;
		gap: var(--space-sm);
		margin-top: var(--space-lg);
	}

	.empty-state {
		text-align: center;
		padding: var(--space-2xl);
		color: var(--color-text-muted);
	}

	@media (max-width: 768px) {
		.summary-grid { grid-template-columns: repeat(2, 1fr); }
		.svc-header, .svc-row { grid-template-columns: 1fr 1fr 1fr; }
		.svc-header span:nth-child(4), .svc-row span:nth-child(4),
		.svc-header span:nth-child(2), .svc-row span:nth-child(2) { display: none; }
	}
</style>
