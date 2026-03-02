<script lang="ts">
	import { api, type LogEntry } from '$lib/api';
	import { onMount } from 'svelte';

	let logs = $state<LogEntry[]>([]);
	let error = $state('');
	let loading = $state(true);
	let search = $state('');
	let level = $state('');
	let limit = $state(200);

	onMount(load);

	async function load() {
		loading = true;
		try {
			logs = await api.getSystemLogs({
				search: search || undefined,
				level: level || undefined,
				limit,
			});
			error = '';
		} catch (e: any) {
			error = e.message;
		} finally {
			loading = false;
		}
	}

	function levelColor(lvl: string): string {
		switch (lvl) {
			case 'error': return '#ef4444';
			case 'warn': return '#f59e0b';
			case 'info': return '#3b82f6';
			case 'debug': return '#94a3b8';
			default: return 'var(--color-text-muted)';
		}
	}
</script>

<div class="max-w-6xl mx-auto p-6">
	<h2 class="text-2xl font-bold mb-6">System Logs</h2>

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid #ef4444;">
			<p style="color: #ef4444;">{error}</p>
		</div>
	{/if}

	<div class="flex gap-3 mb-4 items-end">
		<div class="flex-1">
			<label for="search" class="block text-xs mb-1" style="color: var(--color-text-muted);">Search</label>
			<input id="search" type="text" bind:value={search} placeholder="Filter messages..."
				class="w-full rounded px-3 py-2 text-sm" style="background-color: var(--color-surface); border: 1px solid var(--color-border);" />
		</div>
		<div>
			<label for="level" class="block text-xs mb-1" style="color: var(--color-text-muted);">Level</label>
			<select id="level" bind:value={level}
				class="rounded px-3 py-2 text-sm" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<option value="">All</option>
				<option value="error">Error</option>
				<option value="warn">Warn</option>
				<option value="info">Info</option>
				<option value="debug">Debug</option>
			</select>
		</div>
		<button onclick={load}
			class="px-4 py-2 rounded text-sm font-medium text-white"
			style="background-color: var(--color-primary);">
			{loading ? 'Loading...' : 'Refresh'}
		</button>
	</div>

	<div class="rounded-lg overflow-hidden" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
		<div class="overflow-auto" style="max-height: 70vh;">
			<table class="w-full text-xs font-mono">
				<thead class="sticky top-0" style="background-color: var(--color-surface);">
					<tr style="border-bottom: 1px solid var(--color-border);">
						<th class="text-left p-2" style="color: var(--color-text-muted);">Time</th>
						<th class="text-left p-2" style="color: var(--color-text-muted);">Level</th>
						<th class="text-left p-2" style="color: var(--color-text-muted);">Service</th>
						<th class="text-left p-2" style="color: var(--color-text-muted);">Node</th>
						<th class="text-left p-2" style="color: var(--color-text-muted);">Message</th>
					</tr>
				</thead>
				<tbody>
					{#each logs as log}
						<tr style="border-bottom: 1px solid var(--color-border);">
							<td class="p-2 whitespace-nowrap" style="color: var(--color-text-muted);">{new Date(log.timestamp).toLocaleString()}</td>
							<td class="p-2">
								<span class="px-1.5 py-0.5 rounded text-xs font-medium" style="color: {levelColor(log.level)};">
									{log.level || 'info'}
								</span>
							</td>
							<td class="p-2" style="color: var(--color-text-muted);">{log.service_name}</td>
							<td class="p-2" style="color: var(--color-text-muted);">{log.node_id ? log.node_id.slice(0, 8) : '-'}</td>
							<td class="p-2 whitespace-pre-wrap break-all">{log.message}</td>
						</tr>
					{/each}
				</tbody>
			</table>
			{#if logs.length === 0 && !loading}
				<p class="p-6 text-sm text-center" style="color: var(--color-text-muted);">No logs found</p>
			{/if}
		</div>
	</div>
</div>
