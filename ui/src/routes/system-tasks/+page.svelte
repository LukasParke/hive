<script lang="ts">
	import { api, type SystemTask } from '$lib/api';
import { Badge, EmptyState } from '$lib/components';

	let { data } = $props();

let tasks: SystemTask[] = $state([]);
	let loading = $state('');
	let error = $state('');
	let categoryFilter = $state('all');

	const categories = $derived(() => {
		const cats = new Set(tasks.map(t => t.category));
		return ['all', ...Array.from(cats).sort()];
	});

	let filteredTasks = $derived(
		categoryFilter === 'all'
			? tasks
			: tasks.filter(t => t.category === categoryFilter)
	);

	function formatInterval(seconds: number): string {
		if (seconds < 60) return `${seconds}s`;
		if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
		if (seconds < 86400) return `${Math.round(seconds / 3600)}h`;
		return `${Math.round(seconds / 86400)}d`;
	}

	function formatDuration(ms: number): string {
		if (ms < 1000) return `${ms}ms`;
		if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
		return `${(ms / 60000).toFixed(1)}m`;
	}

	function timeAgo(dateStr: string | null): string {
		if (!dateStr) return 'Never';
		const date = new Date(dateStr);
		const now = new Date();
		const diffMs = now.getTime() - date.getTime();
		const diffSec = Math.floor(diffMs / 1000);
		if (diffSec < 5) return 'Just now';
		if (diffSec < 60) return `${diffSec}s ago`;
		const diffMin = Math.floor(diffSec / 60);
		if (diffMin < 60) return `${diffMin}m ago`;
		const diffH = Math.floor(diffMin / 60);
		if (diffH < 24) return `${diffH}h ago`;
		return `${Math.floor(diffH / 24)}d ago`;
	}

	function statusColor(status: string): string {
		switch (status) {
			case 'success': return 'var(--color-success, #22c55e)';
			case 'error': return 'var(--color-danger, #ef4444)';
			case 'running': return 'var(--color-info, #3b82f6)';
			default: return 'var(--color-muted, #6b7280)';
		}
	}

	function statusBadge(status: string): 'success' | 'danger' | 'info' | 'warning' | 'neutral' {
		switch (status) {
			case 'success': return 'success';
			case 'error': return 'danger';
			case 'running': return 'info';
			default: return 'neutral';
		}
	}

	function categoryIcon(cat: string): string {
		switch (cat) {
			case 'monitoring': return '&#x1f50d;';
			case 'updates': return '&#x21bb;';
			case 'deployment': return '&#x1f680;';
			case 'networking': return '&#x1f310;';
			case 'maintenance': return '&#x1f527;';
			default: return '&#x2699;';
		}
	}

	async function trigger(taskId: string) {
		loading = taskId;
		error = '';
		try {
			await api.triggerSystemTask(taskId);
			await new Promise(r => setTimeout(r, 1500));
			const updated = await api.listSystemTasks();
			tasks = updated;
		} catch (e: any) {
			error = e.message || 'Failed to trigger task';
		} finally {
			loading = '';
		}
	}

	async function toggleEnabled(task: SystemTask) {
		error = '';
		try {
			const updated = await api.updateSystemTask(task.id, { enabled: !task.enabled });
			tasks = tasks.map(t => t.id === task.id ? updated : t);
		} catch (e: any) {
			error = e.message || 'Failed to update task';
		}
	}

	let refreshInterval: ReturnType<typeof setInterval>;
$effect(() => {
	if (tasks.length === 0) {
		tasks = data.tasks || [];
	}
});
	$effect(() => {
		refreshInterval = setInterval(async () => {
			try {
				const updated = await api.listSystemTasks();
				tasks = updated;
			} catch {}
		}, 10000);
		return () => clearInterval(refreshInterval);
	});

	const summaryStats = $derived(() => {
		const total = tasks.length;
		const enabled = tasks.filter(t => t.enabled).length;
		const healthy = tasks.filter(t => t.last_status === 'success').length;
		const errors = tasks.filter(t => t.last_status === 'error').length;
		const totalRuns = tasks.reduce((sum, t) => sum + t.run_count, 0);
		return { total, enabled, healthy, errors, totalRuns };
	});
</script>

<svelte:head><title>System Tasks | Hive</title></svelte:head>

<div class="max-w-6xl mx-auto">
	<div class="page-header">
		<div>
			<h2 class="page-title">System Tasks</h2>
			<p class="page-subtitle">Scheduled jobs that keep the platform healthy and in sync</p>
		</div>
	</div>

	{#if error}
		<div class="alert alert-error mb-4">
			{error}
			<button onclick={() => error = ''} style="float: right; background: none; border: none; color: inherit; cursor: pointer; font-weight: 600;">&times;</button>
		</div>
	{/if}

	<!-- Summary Cards -->
	<div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 12px; margin-bottom: 24px;">
		<div class="stat-card">
			<div class="stat-value">{summaryStats().total}</div>
			<div class="stat-label">Total Tasks</div>
		</div>
		<div class="stat-card">
			<div class="stat-value">{summaryStats().enabled}</div>
			<div class="stat-label">Enabled</div>
		</div>
		<div class="stat-card">
			<div class="stat-value" style="color: var(--color-success, #22c55e)">{summaryStats().healthy}</div>
			<div class="stat-label">Healthy</div>
		</div>
		<div class="stat-card">
			<div class="stat-value" style="color: {summaryStats().errors > 0 ? 'var(--color-danger, #ef4444)' : 'inherit'}">{summaryStats().errors}</div>
			<div class="stat-label">Errors</div>
		</div>
		<div class="stat-card">
			<div class="stat-value">{summaryStats().totalRuns.toLocaleString()}</div>
			<div class="stat-label">Total Runs</div>
		</div>
	</div>

	<!-- Category Filter -->
	<div style="display: flex; gap: 8px; margin-bottom: 16px; flex-wrap: wrap;">
		{#each categories() as cat}
			<button
				class="filter-chip"
				class:active={categoryFilter === cat}
				onclick={() => categoryFilter = cat}
			>
				{cat === 'all' ? 'All' : cat.charAt(0).toUpperCase() + cat.slice(1)}
			</button>
		{/each}
	</div>

	<!-- Task List -->
	{#if filteredTasks.length === 0}
		<EmptyState
			title="No system tasks"
			description="System tasks will appear here once the engine registers them."
		/>
	{:else}
		<div class="task-grid">
			{#each filteredTasks as task (task.id)}
				<div class="task-card" class:disabled={!task.enabled}>
					<div class="task-header">
						<div class="task-title-row">
							<span class="task-category-icon">{@html categoryIcon(task.category)}</span>
							<h3 class="task-name">{task.name}</h3>
							<Badge variant={statusBadge(task.last_status)}>{task.last_status}</Badge>
						</div>
						<p class="task-description">{task.description}</p>
					</div>

					<div class="task-meta">
						<div class="meta-row">
							<span class="meta-label">Interval</span>
							<span class="meta-value">{formatInterval(task.interval_seconds)}</span>
						</div>
						<div class="meta-row">
							<span class="meta-label">Last Run</span>
							<span class="meta-value">{timeAgo(task.last_run_at)}</span>
						</div>
						<div class="meta-row">
							<span class="meta-label">Duration</span>
							<span class="meta-value">{task.last_duration_ms > 0 ? formatDuration(task.last_duration_ms) : '--'}</span>
						</div>
						<div class="meta-row">
							<span class="meta-label">Runs</span>
							<span class="meta-value">{task.run_count.toLocaleString()}</span>
						</div>
						{#if task.error_count > 0}
							<div class="meta-row">
								<span class="meta-label">Errors</span>
								<span class="meta-value" style="color: var(--color-danger, #ef4444)">{task.error_count.toLocaleString()}</span>
							</div>
						{/if}
					</div>

					{#if task.last_status === 'error' && task.last_error}
						<div class="task-error">
							{task.last_error}
						</div>
					{/if}

					<div class="task-actions">
						<button
							class="btn btn-sm"
							class:btn-primary={task.enabled}
							class:btn-muted={!task.enabled}
							onclick={() => trigger(task.id)}
							disabled={loading === task.id}
						>
							{loading === task.id ? 'Running...' : 'Run Now'}
						</button>
						<button
							class="btn btn-sm"
							class:btn-secondary={!task.enabled}
							class:btn-danger={task.enabled}
							onclick={() => toggleEnabled(task)}
						>
							{task.enabled ? 'Disable' : 'Enable'}
						</button>
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>

<style>
	.stat-card {
		background: var(--color-surface, #1a1a2e);
		border: 1px solid var(--color-border, #2a2a4a);
		border-radius: 10px;
		padding: 16px;
		text-align: center;
	}
	.stat-value {
		font-size: 1.5rem;
		font-weight: 700;
		line-height: 1.2;
	}
	.stat-label {
		font-size: 0.75rem;
		color: var(--color-muted, #6b7280);
		margin-top: 4px;
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.filter-chip {
		padding: 6px 14px;
		border-radius: 20px;
		border: 1px solid var(--color-border, #2a2a4a);
		background: transparent;
		color: var(--color-text-secondary, #9ca3af);
		font-size: 0.8rem;
		cursor: pointer;
		transition: all 0.15s;
	}
	.filter-chip:hover {
		border-color: var(--color-primary, #6366f1);
		color: var(--color-text, #e5e7eb);
	}
	.filter-chip.active {
		background: var(--color-primary, #6366f1);
		border-color: var(--color-primary, #6366f1);
		color: white;
	}

	.task-grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
		gap: 16px;
	}

	.task-card {
		background: var(--color-surface, #1a1a2e);
		border: 1px solid var(--color-border, #2a2a4a);
		border-radius: 12px;
		padding: 20px;
		display: flex;
		flex-direction: column;
		gap: 12px;
		transition: border-color 0.15s;
	}
	.task-card:hover {
		border-color: var(--color-border-hover, #3a3a5a);
	}
	.task-card.disabled {
		opacity: 0.55;
	}

	.task-header {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.task-title-row {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.task-category-icon {
		font-size: 1rem;
		line-height: 1;
	}
	.task-name {
		font-size: 0.95rem;
		font-weight: 600;
		flex: 1;
		margin: 0;
	}
	.task-description {
		font-size: 0.8rem;
		color: var(--color-muted, #6b7280);
		margin: 0;
		line-height: 1.4;
	}

	.task-meta {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 6px 16px;
	}
	.meta-row {
		display: flex;
		justify-content: space-between;
		font-size: 0.8rem;
	}
	.meta-label {
		color: var(--color-muted, #6b7280);
	}
	.meta-value {
		font-weight: 500;
	}

	.task-error {
		font-size: 0.75rem;
		background: rgba(239, 68, 68, 0.1);
		border: 1px solid rgba(239, 68, 68, 0.2);
		border-radius: 6px;
		padding: 8px 10px;
		color: #fca5a5;
		word-break: break-word;
		max-height: 60px;
		overflow: auto;
	}

	.task-actions {
		display: flex;
		gap: 8px;
		margin-top: auto;
		padding-top: 4px;
	}

	.btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		padding: 6px 14px;
		border-radius: 6px;
		font-size: 0.8rem;
		font-weight: 500;
		cursor: pointer;
		border: 1px solid transparent;
		transition: all 0.15s;
	}
	.btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
	.btn-sm {
		padding: 5px 12px;
		font-size: 0.78rem;
	}
	.btn-primary {
		background: var(--color-primary, #6366f1);
		color: white;
		border-color: var(--color-primary, #6366f1);
	}
	.btn-primary:hover:not(:disabled) {
		opacity: 0.9;
	}
	.btn-muted {
		background: var(--color-surface-alt, #2a2a4a);
		color: var(--color-muted, #6b7280);
		border-color: var(--color-border, #2a2a4a);
	}
</style>
