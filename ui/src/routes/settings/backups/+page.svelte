<script lang="ts">
	import { api, type BackupConfig, type BackupRun } from '$lib/api';
	import { onMount } from 'svelte';

	let configs = $state<BackupConfig[]>([]);
	let runs = $state<Record<string, BackupRun[]>>({});
	let error = $state('');
	let showForm = $state(false);
	let triggering = $state('');
	let restoring = $state('');
	let expandedConfig = $state('');

	let form = $state({ resource_id: '', schedule: '0 3 * * *', s3_bucket: '', s3_prefix: 'backups/' });

	onMount(load);

	async function load() {
		try {
			configs = await api.listBackupConfigs();
		} catch (e: any) {
			error = e.message;
		}
	}

	async function create() {
		try {
			await api.createBackupConfig(form);
			showForm = false;
			form = { resource_id: '', schedule: '0 3 * * *', s3_bucket: '', s3_prefix: 'backups/' };
			await load();
		} catch (e: any) {
			error = e.message;
		}
	}

	async function trigger(configId: string) {
		triggering = configId;
		try {
			await api.triggerBackup(configId);
			setTimeout(() => loadRuns(configId), 2000);
		} catch (e: any) {
			error = e.message;
		}
		triggering = '';
	}

	async function loadRuns(configId: string) {
		try {
			const r = await api.listBackupRuns(configId);
			runs = { ...runs, [configId]: r };
		} catch {}
	}

	async function restore(configId: string, runId: string) {
		if (!confirm('Are you sure you want to restore this backup? This will overwrite current data.')) return;
		restoring = runId;
		try {
			await api.restoreBackup(configId, runId);
			await loadRuns(configId);
		} catch (e: any) {
			error = e.message;
		}
		restoring = '';
	}

	async function toggleExpand(configId: string) {
		if (expandedConfig === configId) {
			expandedConfig = '';
		} else {
			expandedConfig = configId;
			await loadRuns(configId);
		}
	}

	function formatBytes(bytes: number): string {
		if (!bytes) return '0 B';
		const k = 1024;
		const sizes = ['B', 'KB', 'MB', 'GB'];
		const i = Math.floor(Math.log(bytes) / Math.log(k));
		return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
	}

	function statusColor(status: string): string {
		switch (status) {
			case 'success': return '#22c55e';
			case 'running': return '#f59e0b';
			case 'failed': return '#ef4444';
			default: return '#94a3b8';
		}
	}
</script>

<div class="max-w-4xl mx-auto p-6">
	<div class="flex items-center justify-between mb-6">
		<h2 class="text-2xl font-bold">Backup Management</h2>
		<button onclick={() => showForm = !showForm}
			class="px-4 py-2 rounded text-sm font-medium text-white"
			style="background-color: var(--color-primary);">
			{showForm ? 'Cancel' : 'New Backup Config'}
		</button>
	</div>

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid #ef4444;">
			<p style="color: #ef4444;">{error}</p>
			<button onclick={() => error = ''} class="text-xs mt-1 underline" style="color: #ef4444;">Dismiss</button>
		</div>
	{/if}

	{#if showForm}
		<div class="rounded-lg p-5 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<div class="grid grid-cols-2 gap-4">
				<div>
					<label for="res-id" class="block text-sm mb-1" style="color: var(--color-text-muted);">Database ID</label>
					<input id="res-id" type="text" bind:value={form.resource_id} placeholder="database-id"
						class="w-full rounded px-3 py-2 text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
				<div>
					<label for="sched" class="block text-sm mb-1" style="color: var(--color-text-muted);">Cron Schedule</label>
					<input id="sched" type="text" bind:value={form.schedule} placeholder="0 3 * * *"
						class="w-full rounded px-3 py-2 text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
				<div>
					<label for="s3-bucket" class="block text-sm mb-1" style="color: var(--color-text-muted);">S3 Bucket</label>
					<input id="s3-bucket" type="text" bind:value={form.s3_bucket} placeholder="my-backups"
						class="w-full rounded px-3 py-2 text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
				<div>
					<label for="s3-prefix" class="block text-sm mb-1" style="color: var(--color-text-muted);">S3 Prefix</label>
					<input id="s3-prefix" type="text" bind:value={form.s3_prefix} placeholder="backups/"
						class="w-full rounded px-3 py-2 text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
			</div>
			<button onclick={create}
				class="px-4 py-2 rounded text-sm font-medium text-white mt-4"
				style="background-color: var(--color-primary);">
				Create Config
			</button>
		</div>
	{/if}

	<div class="space-y-3">
		{#each configs as cfg}
			<div class="rounded-lg" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<div class="p-4 flex items-center justify-between">
					<button onclick={() => toggleExpand(cfg.id)} class="flex items-center gap-3 text-left flex-1">
						<span class="text-sm font-mono" style="color: var(--color-text-muted);">{cfg.id.slice(0, 8)}</span>
						<span class="text-sm font-medium">{cfg.schedule}</span>
						{#if cfg.s3_bucket}
							<span class="text-xs" style="color: var(--color-text-muted);">s3://{cfg.s3_bucket}/{cfg.s3_prefix}</span>
						{/if}
					</button>
					<button onclick={() => trigger(cfg.id)} disabled={triggering === cfg.id}
						class="px-3 py-1 rounded text-xs font-medium text-white"
						style="background-color: var(--color-primary);">
						{triggering === cfg.id ? 'Running...' : 'Trigger Now'}
					</button>
				</div>

				{#if expandedConfig === cfg.id}
					<div class="px-4 pb-4">
						<div class="border-t pt-3" style="border-color: var(--color-border);">
							<p class="text-xs font-semibold mb-2" style="color: var(--color-text-muted);">Backup Runs</p>
							{#if (runs[cfg.id] ?? []).length === 0}
								<p class="text-xs" style="color: var(--color-text-muted);">No runs yet</p>
							{/if}
							{#each runs[cfg.id] ?? [] as run}
								<div class="flex items-center justify-between py-1.5 text-xs" style="border-bottom: 1px solid var(--color-border);">
									<div class="flex items-center gap-2">
										<span class="inline-block w-2 h-2 rounded-full" style="background-color: {statusColor(run.status)};"></span>
										<span>{run.status}</span>
									</div>
									<span style="color: var(--color-text-muted);">{formatBytes(run.size)}</span>
									<span style="color: var(--color-text-muted);">{new Date(run.started_at).toLocaleString()}</span>
									{#if run.status === 'success'}
										<button onclick={() => restore(cfg.id, run.id)}
											class="px-2 py-0.5 rounded text-xs font-medium"
											style="background-color: var(--color-warning); color: var(--color-bg);"
											disabled={restoring === run.id}>
											{restoring === run.id ? 'Restoring...' : 'Restore'}
										</button>
									{/if}
								</div>
							{/each}
						</div>
					</div>
				{/if}
			</div>
		{/each}
		{#if configs.length === 0 && !showForm}
			<div class="text-center py-12">
				<p class="text-lg mb-2" style="color: var(--color-text-muted);">No backup configurations</p>
				<p class="text-sm" style="color: var(--color-text-muted);">Create a backup config to start protecting your data</p>
			</div>
		{/if}
	</div>
</div>
