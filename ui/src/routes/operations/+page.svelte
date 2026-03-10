<script lang="ts">
	import { Badge, Card, EmptyState } from '$lib/components';
	import type { App, Stack, SystemTask, UpdatesSummary, BackupConfig, DNSProvider, GitSource } from '$lib/types';

	let { data } = $props();

	const apps = $derived((data.apps ?? []) as (App & { project_name: string })[]);
	const stacks = $derived((data.stacks ?? []) as (Stack & { project_name: string })[]);
	const tasks = $derived((data.tasks ?? []) as SystemTask[]);
	const updates = $derived(data.updates as UpdatesSummary);
	const backups = $derived((data.backups ?? []) as BackupConfig[]);
	const gitSources = $derived((data.gitSources ?? []) as GitSource[]);
	const dnsProviders = $derived((data.dnsProviders ?? []) as DNSProvider[]);
	const networking = $derived((data.networking ?? {}) as Record<string, any>);

	const failingApps = $derived(apps.filter((a) => ['failed', 'error', 'degraded'].includes((a.status || '').toLowerCase())));
	const unhealthyStacks = $derived(stacks.filter((s) => ['failed', 'error', 'degraded'].includes((s.status || '').toLowerCase())));
	const failingTasks = $derived(tasks.filter((t) => t.last_status === 'error'));
	const disabledTasks = $derived(tasks.filter((t) => !t.enabled));
	const activeGitSources = $derived(gitSources.length);
	const activeDNSProviders = $derived(dnsProviders.length);
	const ingressMode = $derived((networking.ingress_mode as string) || 'direct');

	function statusVariant(status: string): 'success' | 'warning' | 'danger' | 'neutral' {
		switch ((status || '').toLowerCase()) {
			case 'running':
			case 'healthy':
			case 'success':
				return 'success';
			case 'degraded':
			case 'pending':
			case 'deploying':
			case 'building':
				return 'warning';
			case 'failed':
			case 'error':
				return 'danger';
			default:
				return 'neutral';
		}
	}
</script>

<svelte:head>
	<title>Operations | Hive</title>
</svelte:head>

<div class="max-w-7xl mx-auto">
	<div class="page-header">
		<div>
			<h1 class="page-title">Operations Center</h1>
			<p class="page-subtitle">Cross-platform health for deploys, updates, tasks, and backups</p>
		</div>
	</div>

	<div class="ops-grid mb-4">
		<Card>
			<div class="metric">
				<div class="metric-value">{failingApps.length}</div>
				<div class="metric-label">Failing Apps</div>
			</div>
		</Card>
		<Card>
			<div class="metric">
				<div class="metric-value">{unhealthyStacks.length}</div>
				<div class="metric-label">Unhealthy Stacks</div>
			</div>
		</Card>
		<Card>
			<div class="metric">
				<div class="metric-value">{failingTasks.length}</div>
				<div class="metric-label">Task Errors</div>
			</div>
		</Card>
		<Card>
			<div class="metric">
				<div class="metric-value">{updates.pending_updates}</div>
				<div class="metric-label">Pending Node Updates</div>
			</div>
		</Card>
		<Card>
			<div class="metric">
				<div class="metric-value">{updates.service_updates}</div>
				<div class="metric-label">Service Image Updates</div>
			</div>
		</Card>
		<Card>
			<div class="metric">
				<div class="metric-value">{backups.length}</div>
				<div class="metric-label">Backup Policies</div>
			</div>
		</Card>
	</div>

	<div class="panel-grid">
		<Card>
			<h3 class="panel-title">Failing Apps</h3>
			{#if failingApps.length === 0}
				<p class="muted">No failing apps.</p>
			{:else}
				<div class="list">
					{#each failingApps.slice(0, 10) as app (app.id)}
						<a class="row" href={`/projects/${app.project_id}/apps/${app.id}`}>
							<div>
								<div class="row-title">{app.name}</div>
								<div class="row-sub">{app.project_name}</div>
							</div>
							<Badge variant={statusVariant(app.status)}>{app.status}</Badge>
						</a>
					{/each}
				</div>
			{/if}
		</Card>

		<Card>
			<h3 class="panel-title">Unhealthy Stacks</h3>
			{#if unhealthyStacks.length === 0}
				<p class="muted">No degraded or failed stacks.</p>
			{:else}
				<div class="list">
					{#each unhealthyStacks.slice(0, 10) as stack (stack.id)}
						<a class="row" href={`/projects/${stack.project_id}/stacks`}>
							<div>
								<div class="row-title">{stack.name}</div>
								<div class="row-sub">{stack.project_name}</div>
							</div>
							<Badge variant={statusVariant(stack.status)}>{stack.status}</Badge>
						</a>
					{/each}
				</div>
			{/if}
		</Card>

		<Card>
			<h3 class="panel-title">System Task Issues</h3>
			{#if failingTasks.length === 0 && disabledTasks.length === 0}
				<p class="muted">All enabled tasks are healthy.</p>
			{:else}
				<div class="list">
					{#each failingTasks.slice(0, 8) as task (task.id)}
						<a class="row" href="/system-tasks">
							<div>
								<div class="row-title">{task.name}</div>
								<div class="row-sub">{task.last_error || 'Last run failed'}</div>
							</div>
							<Badge variant="danger">error</Badge>
						</a>
					{/each}
					{#each disabledTasks.slice(0, 4) as task (task.id)}
						<a class="row" href="/system-tasks">
							<div>
								<div class="row-title">{task.name}</div>
								<div class="row-sub">Task is disabled</div>
							</div>
							<Badge variant="warning">disabled</Badge>
						</a>
					{/each}
				</div>
			{/if}
		</Card>

		<Card>
			<h3 class="panel-title">Integrations</h3>
			<div class="meta-list">
				<div class="meta-item">
					<span>Git Sources</span>
					<Badge variant={activeGitSources > 0 ? 'success' : 'warning'}>{activeGitSources}/{gitSources.length} enabled</Badge>
				</div>
				<div class="meta-item">
					<span>DNS Providers</span>
					<Badge variant={activeDNSProviders > 0 ? 'success' : 'warning'}>{activeDNSProviders}/{dnsProviders.length} enabled</Badge>
				</div>
				<div class="meta-item">
					<span>Ingress Mode</span>
					<Badge variant={ingressMode === 'cloudflare_tunnel' ? 'success' : 'neutral'}>{ingressMode}</Badge>
				</div>
			</div>
			<div class="actions">
				<a class="btn btn-ghost btn-sm" href="/git">Git</a>
				<a class="btn btn-ghost btn-sm" href="/dns">DNS</a>
				<a class="btn btn-ghost btn-sm" href="/networking">Ingress</a>
				<a class="btn btn-ghost btn-sm" href="/backups">Backups</a>
			</div>
		</Card>
	</div>

	{#if apps.length === 0 && stacks.length === 0}
		<div class="mt-4">
			<EmptyState
				title="No workloads yet"
				description="Deploy an app or stack from the catalog to start monitoring platform operations."
			/>
		</div>
	{/if}
</div>

<style>
	.ops-grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
		gap: 12px;
	}
	.panel-grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
		gap: 12px;
	}
	.metric {
		text-align: center;
		padding: 8px 4px;
	}
	.metric-value {
		font-size: 1.5rem;
		font-weight: 700;
	}
	.metric-label {
		font-size: 0.78rem;
		color: var(--color-text-muted);
	}
	.panel-title {
		font-size: 0.95rem;
		font-weight: 600;
		margin-bottom: 10px;
	}
	.muted {
		color: var(--color-text-muted);
		font-size: 0.9rem;
	}
	.list {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.row {
		display: flex;
		justify-content: space-between;
		gap: 8px;
		align-items: center;
		padding: 8px 10px;
		border-radius: 10px;
		border: 1px solid var(--color-border);
		text-decoration: none;
		color: inherit;
	}
	.row:hover {
		border-color: var(--color-border-strong);
		background: var(--color-surface-2);
	}
	.row-title {
		font-size: 0.9rem;
		font-weight: 600;
	}
	.row-sub {
		font-size: 0.78rem;
		color: var(--color-text-muted);
	}
	.meta-list {
		display: grid;
		gap: 8px;
		margin-bottom: 10px;
	}
	.meta-item {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		font-size: 0.86rem;
	}
	.actions {
		display: flex;
		gap: 8px;
		flex-wrap: wrap;
	}
</style>
