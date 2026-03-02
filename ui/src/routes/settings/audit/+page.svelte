<script lang="ts">
	import { api, type AuditLogEntry } from '$lib/api';
	import { onMount } from 'svelte';

	let logs = $state<AuditLogEntry[]>([]);
	let stats = $state<Record<string, number>>({});
	let error = $state('');
	let filterUserId = $state('');
	let filterAction = $state('');
	let filterResource = $state('');

	onMount(load);

	async function load() {
		try {
			const params = new URLSearchParams();
			if (filterUserId) params.set('user_id', filterUserId);
			if (filterAction) params.set('action', filterAction);
			if (filterResource) params.set('resource', filterResource);
			params.set('limit', '100');
			logs = await api.listAuditLogs(params.toString());
			stats = await api.getAuditLogStats();
		} catch (e: any) {
			error = e.message;
		}
	}

	function applyFilters() {
		load();
	}
</script>

<div class="max-w-4xl mx-auto p-6">
	<h2 class="text-2xl font-bold mb-6">Audit Log</h2>

	{#if error}
		<div
			class="rounded-lg p-4 mb-4"
			style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid #ef4444;"
		>
			<p style="color: #ef4444;">{error}</p>
			<button onclick={() => (error = '')} class="text-xs mt-1 underline" style="color: #ef4444;"
				>Dismiss</button
			>
		</div>
	{/if}

	{#if Object.keys(stats).length > 0}
		<div
			class="rounded-lg p-4 mb-6 flex flex-wrap gap-4"
			style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
		>
			{#each Object.entries(stats) as [action, count]}
				<span class="text-sm" style="color: var(--color-text-muted);">
					<span class="font-medium">{action}</span>: {count}
				</span>
			{/each}
		</div>
	{/if}

	<div
		class="rounded-lg p-4 mb-6 flex flex-wrap gap-4 items-end"
		style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
	>
		<div>
			<label class="block text-xs mb-1" style="color: var(--color-text-muted);">User ID</label>
			<input
				type="text"
				bind:value={filterUserId}
				placeholder="Filter by user"
				class="rounded px-2 py-1 text-sm w-40"
				style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
			/>
		</div>
		<div>
			<label class="block text-xs mb-1" style="color: var(--color-text-muted);">Action</label>
			<select
				bind:value={filterAction}
				class="rounded px-2 py-1 text-sm"
				style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
			>
				<option value="">All</option>
				<option value="POST">POST</option>
				<option value="PUT">PUT</option>
				<option value="DELETE">DELETE</option>
			</select>
		</div>
		<div>
			<label class="block text-xs mb-1" style="color: var(--color-text-muted);">Resource</label>
			<input
				type="text"
				bind:value={filterResource}
				placeholder="Filter by path"
				class="rounded px-2 py-1 text-sm w-48"
				style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
			/>
		</div>
		<button
			onclick={applyFilters}
			class="px-4 py-2 rounded text-sm font-medium text-white"
			style="background-color: var(--color-primary);"
		>
			Apply
		</button>
	</div>

	<div
		class="rounded-lg overflow-hidden"
		style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
	>
		<div class="overflow-x-auto">
			<table class="w-full text-sm">
				<thead>
					<tr style="border-bottom: 1px solid var(--color-border);">
						<th class="text-left p-3" style="color: var(--color-text-muted);">Time</th>
						<th class="text-left p-3" style="color: var(--color-text-muted);">User</th>
						<th class="text-left p-3" style="color: var(--color-text-muted);">Action</th>
						<th class="text-left p-3" style="color: var(--color-text-muted);">Resource</th>
						<th class="text-left p-3" style="color: var(--color-text-muted);">ID</th>
					</tr>
				</thead>
				<tbody>
					{#each logs as log}
						<tr style="border-bottom: 1px solid var(--color-border);">
							<td class="p-3" style="color: var(--color-text-muted);">
								{new Date(log.created_at).toLocaleString()}
							</td>
							<td class="p-3 font-mono text-xs">{log.user_id.slice(0, 8)}...</td>
							<td class="p-3">{log.action}</td>
							<td class="p-3 font-mono text-xs max-w-xs truncate" title={log.resource}>
								{log.resource}
							</td>
							<td class="p-3 font-mono text-xs">{log.resource_id || '-'}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	</div>

	{#if logs.length === 0}
		<div class="text-center py-12">
			<p class="text-lg mb-2" style="color: var(--color-text-muted);">No audit log entries</p>
			<p class="text-sm" style="color: var(--color-text-muted);">Mutating requests will be logged here</p>
		</div>
	{/if}
</div>
