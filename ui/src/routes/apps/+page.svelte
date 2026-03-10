<script lang="ts">
	import { api } from '$lib/api';
	import { Badge, Button, EmptyState } from '$lib/components';
	import { invalidateAll } from '$app/navigation';

	let { data } = $props();

	let error = $state('');
	let statusFilter = $state('all');
	let actionLoading = $state('');

	const statuses = ['all', 'running', 'degraded', 'failed', 'stopped', 'deploying', 'building', 'pending'];

	type UnifiedItem = {
		kind: 'app' | 'stack';
		id: string;
		project_id: string;
		project_name: string;
		name: string;
		status: string;
		deploy_type?: string;
		domain?: string;
		port?: number;
	};

	let items = $derived.by(() => {
		const appItems: UnifiedItem[] = data.apps.map((a: any) => ({
			kind: 'app' as const,
			id: a.id,
			project_id: a.project_id,
			project_name: a.project_name,
			name: a.name,
			status: a.status,
			deploy_type: a.deploy_type,
			domain: a.domain,
			port: a.port,
		}));
		const stackItems: UnifiedItem[] = data.stacks.map((s: any) => ({
			kind: 'stack' as const,
			id: s.id,
			project_id: s.project_id,
			project_name: s.project_name,
			name: s.name,
			status: s.status,
		}));
		return [...appItems, ...stackItems];
	});

	let filteredItems = $derived(
		statusFilter === 'all'
			? items
			: items.filter((i) => i.status === statusFilter)
	);

	let statusCounts = $derived.by(() => {
		const counts: Record<string, number> = { all: items.length };
		for (const item of items) {
			counts[item.status] = (counts[item.status] || 0) + 1;
		}
		return counts;
	});

	async function deployApp(projectId: string, appId: string) {
		actionLoading = appId;
		try {
			await api.deployApp(projectId, appId);
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		}
		actionLoading = '';
	}

	async function deleteApp(projectId: string, appId: string, name: string) {
		if (!confirm(`Delete app "${name}"? This will remove the Docker service and all associated data.`)) return;
		actionLoading = appId;
		try {
			await api.deleteApp(projectId, appId);
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		}
		actionLoading = '';
	}

	async function deleteStack(projectId: string, stackId: string, name: string) {
		if (!confirm(`Remove stack "${name}"? This will remove all stack services and data.`)) return;
		actionLoading = stackId;
		try {
			await api.deleteStack(projectId, stackId);
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		}
		actionLoading = '';
	}

	function statusColor(status: string): string {
		switch (status) {
			case 'running': return 'var(--color-success)';
			case 'building':
			case 'deploying':
			case 'pending':
			case 'starting':
				return 'var(--color-warning)';
			case 'degraded':
				return 'rgb(249, 115, 22)';
			case 'failed':
			case 'stopped':
				return 'var(--color-danger)';
			default: return 'var(--color-text-muted)';
		}
	}

	function statusBg(status: string): string {
		switch (status) {
			case 'running': return 'rgba(34, 197, 94, 0.12)';
			case 'building':
			case 'deploying':
			case 'pending':
			case 'starting':
				return 'rgba(229, 160, 13, 0.12)';
			case 'degraded':
				return 'rgba(249, 115, 22, 0.12)';
			case 'failed':
			case 'stopped':
				return 'rgba(239, 68, 68, 0.12)';
			default: return 'rgba(154, 145, 138, 0.12)';
		}
	}
</script>

<svelte:head><title>Apps | Hive</title></svelte:head>

<div class="max-w-6xl mx-auto">
	<div class="page-header">
		<div>
			<h2 class="page-title">Apps & Stacks</h2>
			<p class="page-subtitle">{items.length} item{items.length !== 1 ? 's' : ''} across all projects</p>
		</div>
	</div>

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);">
			<p style="color: var(--color-danger);">{error}</p>
		</div>
	{/if}

	<!-- Status filters -->
	<div class="flex gap-2 flex-wrap mb-6">
		{#each statuses as s}
			{@const count = statusCounts[s] || 0}
			{#if s === 'all' || count > 0}
				<button
					onclick={() => statusFilter = s}
					class="filter-btn"
					class:active={statusFilter === s}
				>
					{s.charAt(0).toUpperCase() + s.slice(1)}
					<span class="filter-count">{count}</span>
				</button>
			{/if}
		{/each}
	</div>

	{#if filteredItems.length > 0}
		<div class="space-y-3">
			{#each filteredItems as item}
				<div class="app-row">
					<div class="min-w-0 flex-1">
						<div class="flex items-center gap-3 flex-wrap">
							{#if item.kind === 'app'}
								<a href="/projects/{item.project_id}/apps/{item.id}" class="font-semibold hover:underline text-sm">{item.name}</a>
							{:else}
								<a href="/projects/{item.project_id}/stacks" class="font-semibold hover:underline text-sm">{item.name}</a>
							{/if}
							<span
								class="px-2 py-0.5 rounded text-xs font-semibold uppercase"
								style="background-color: {statusBg(item.status)}; color: {statusColor(item.status)};"
							>
								{item.status}
							</span>
							{#if item.kind === 'stack'}
								<span class="kind-badge stack">Stack</span>
							{:else if item.deploy_type}
								<span class="text-xs px-2 py-0.5 rounded" style="background-color: var(--color-bg); color: var(--color-text-muted);">{item.deploy_type}</span>
							{/if}
						</div>
						<div class="flex items-center gap-3 mt-1 flex-wrap">
							<a href="/projects/{item.project_id}" class="text-xs hover:underline" style="color: var(--color-text-muted);">{item.project_name}</a>
							{#if item.domain}
								<a href="https://{item.domain}" target="_blank" class="text-xs underline truncate" style="color: var(--color-primary);">{item.domain}</a>
							{/if}
							{#if item.port}
								<span class="text-xs font-mono" style="color: var(--color-text-muted);">:{item.port}</span>
							{/if}
						</div>
					</div>
					<div class="flex gap-2 shrink-0 flex-wrap">
						{#if item.kind === 'app'}
							<a href="/projects/{item.project_id}/apps/{item.id}/logs" class="action-btn" style="border: 1px solid var(--color-border); color: var(--color-text-muted);">
								Logs
							</a>
							<button
								onclick={() => deployApp(item.project_id, item.id)}
								disabled={actionLoading === item.id}
								class="action-btn"
								style="background-color: var(--color-primary); color: var(--color-bg);"
							>
								{#if item.status === 'failed'}
									{actionLoading === item.id ? '...' : 'Retry'}
								{:else}
									{actionLoading === item.id ? '...' : 'Deploy'}
								{/if}
							</button>
							<button
								onclick={() => deleteApp(item.project_id, item.id, item.name)}
								disabled={actionLoading === item.id}
								class="action-btn"
								style="border: 1px solid rgba(239,68,68,0.3); color: var(--color-danger);"
							>
								Delete
							</button>
						{:else}
							<a href="/projects/{item.project_id}/stacks" class="action-btn" style="border: 1px solid var(--color-border); color: var(--color-text-muted);">
								View
							</a>
							<button
								onclick={() => deleteStack(item.project_id, item.id, item.name)}
								disabled={actionLoading === item.id}
								class="action-btn"
								style="border: 1px solid rgba(239,68,68,0.3); color: var(--color-danger);"
							>
								Remove
							</button>
						{/if}
					</div>
				</div>
			{/each}
		</div>
	{:else if statusFilter !== 'all'}
		<EmptyState title="No {statusFilter} items" description="No apps or stacks match the selected filter." />
	{:else}
		<EmptyState title="No apps yet" description="Deploy an app from the Catalog or create one in a project." />
	{/if}
</div>

<style>
	.filter-btn {
		display: inline-flex;
		align-items: center;
		gap: 0.375rem;
		padding: 0.375rem 0.75rem;
		border-radius: var(--radius-lg);
		font-size: var(--text-xs);
		font-weight: 500;
		cursor: pointer;
		background-color: var(--color-surface);
		border: 1px solid var(--color-border);
		color: var(--color-text-muted);
		transition: all var(--transition-base);
	}
	.filter-btn:hover {
		border-color: var(--color-primary-border);
		color: var(--color-text);
	}
	.filter-btn.active {
		background-color: var(--color-primary-dim);
		border-color: var(--color-primary-border);
		color: var(--color-primary);
	}
	.filter-count {
		font-size: 0.625rem;
		font-weight: 600;
		padding: 0 0.25rem;
		border-radius: var(--radius-sm);
		background-color: var(--color-bg);
		font-variant-numeric: tabular-nums;
	}
	.active .filter-count {
		background-color: rgba(var(--color-primary-rgb, 59, 130, 246), 0.15);
	}
	.app-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
		padding: 1rem;
		border-radius: var(--radius-lg);
		background-color: var(--color-surface);
		border: 1px solid var(--color-border);
	}
	.action-btn {
		padding: 0.375rem 0.75rem;
		border-radius: var(--radius-lg);
		font-size: var(--text-xs);
		font-weight: 500;
		cursor: pointer;
		white-space: nowrap;
		transition: all var(--transition-base);
		text-decoration: none;
	}
	.action-btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
	.kind-badge {
		display: inline-flex;
		align-items: center;
		padding: 0.125rem 0.5rem;
		border-radius: var(--radius-sm);
		font-size: 0.625rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}
	.kind-badge.stack {
		background-color: rgba(139, 92, 246, 0.12);
		color: rgb(139, 92, 246);
	}

	@media (max-width: 768px) {
		.app-row {
			flex-direction: column;
			align-items: stretch;
		}
		.action-btn {
			min-height: 40px;
			padding: 0.5rem 0.875rem;
			font-size: var(--text-sm);
		}
	}
</style>
