<script lang="ts">
	import { api, type MaintenanceTask, type MaintenanceRun } from '$lib/api';
	import { onMount } from 'svelte';

	let tasks = $state<MaintenanceTask[]>([]);
	let runs = $state<Record<string, MaintenanceRun[]>>({});
	let error = $state('');
	let showForm = $state(false);
	let triggering = $state('');
	let expandedTask = $state('');

	let newType = $state('image_prune');
	let newSchedule = $state('0 3 * * 0');

	const taskTypes = [
		{ value: 'image_prune', label: 'Docker Image Prune' },
		{ value: 'db_vacuum', label: 'Database VACUUM' },
	];

	onMount(load);

	async function load() {
		try {
			tasks = await api.listMaintenanceTasks();
		} catch (e: any) {
			error = e.message;
		}
	}

	async function create() {
		try {
			await api.createMaintenanceTask({
				type: newType,
				schedule: newSchedule,
				config: {}
			});
			showForm = false;
			newType = 'image_prune';
			newSchedule = '0 3 * * 0';
			await load();
		} catch (e: any) {
			error = e.message;
		}
	}

	async function trigger(taskId: string) {
		triggering = taskId;
		try {
			await api.triggerMaintenanceTask(taskId);
			setTimeout(() => loadRuns(taskId), 2000);
		} catch (e: any) {
			error = e.message;
		}
		triggering = '';
	}

	async function loadRuns(taskId: string) {
		try {
			const r = await api.listMaintenanceRuns(taskId);
			runs = { ...runs, [taskId]: r };
		} catch {}
	}

	async function toggleExpand(taskId: string) {
		if (expandedTask === taskId) {
			expandedTask = '';
		} else {
			expandedTask = taskId;
			await loadRuns(taskId);
		}
	}

	async function remove(taskId: string) {
		if (!confirm('Delete this maintenance task?')) return;
		try {
			await api.deleteMaintenanceTask(taskId);
			await load();
		} catch (e: any) {
			error = e.message;
		}
	}

	function statusColor(status: string): string {
		switch (status) {
			case 'success':
				return '#22c55e';
			case 'running':
				return '#f59e0b';
			case 'failed':
				return '#ef4444';
			default:
				return '#94a3b8';
		}
	}

	function typeLabel(type: string): string {
		const t = taskTypes.find((x) => x.value === type);
		return t?.label ?? type;
	}
</script>

<div class="max-w-4xl mx-auto p-6">
	<div class="flex items-center justify-between mb-6">
		<h2 class="text-2xl font-bold">Maintenance Tasks</h2>
		<button
			onclick={() => (showForm = !showForm)}
			class="px-4 py-2 rounded text-sm font-medium text-white"
			style="background-color: var(--color-primary);"
		>
			{showForm ? 'Cancel' : 'New Task'}
		</button>
	</div>

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

	{#if showForm}
		<div
			class="rounded-lg p-5 mb-6"
			style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
		>
			<div class="mb-4">
				<label class="block text-sm mb-1" style="color: var(--color-text-muted);">Task Type</label>
				<select
					bind:value={newType}
					class="rounded px-3 py-2 text-sm w-full"
					style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
				>
					{#each taskTypes as t}
						<option value={t.value}>{t.label}</option>
					{/each}
				</select>
			</div>
			<div class="mb-4">
				<label class="block text-sm mb-1" style="color: var(--color-text-muted);"
					>Cron Schedule</label
				>
				<input
					type="text"
					bind:value={newSchedule}
					placeholder="0 3 * * 0"
					class="w-full rounded px-3 py-2 text-sm"
					style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
				/>
				<p class="text-xs mt-1" style="color: var(--color-text-muted);">
					e.g. 0 3 * * 0 = Sundays at 3am
				</p>
			</div>
			<button
				onclick={create}
				class="px-4 py-2 rounded text-sm font-medium text-white"
				style="background-color: var(--color-primary);"
			>
				Create Task
			</button>
		</div>
	{/if}

	<div class="space-y-3">
		{#each tasks as task}
			<div
				class="rounded-lg"
				style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
			>
				<div class="p-4 flex items-center justify-between">
					<button
						onclick={() => toggleExpand(task.id)}
						class="flex items-center gap-3 text-left flex-1"
					>
						<span class="text-sm font-medium">{typeLabel(task.type)}</span>
						<span class="text-xs font-mono" style="color: var(--color-text-muted);"
							>{task.schedule}</span
						>
						{#if task.last_status}
							<span
								class="text-xs px-2 py-0.5 rounded"
								style="background-color: rgba(34, 197, 94, 0.2); color: #22c55e;"
							>
								Last: {task.last_status}
							</span>
						{/if}
					</button>
					<div class="flex gap-2">
						<button
							onclick={() => trigger(task.id)}
							disabled={triggering === task.id}
							class="px-3 py-1 rounded text-xs font-medium text-white"
							style="background-color: var(--color-primary);"
						>
							{triggering === task.id ? 'Running...' : 'Trigger Now'}
						</button>
						<button
							onclick={() => remove(task.id)}
							class="px-3 py-1 rounded text-xs font-medium"
							style="border: 1px solid #ef4444; color: #ef4444;"
						>
							Delete
						</button>
					</div>
				</div>

				{#if expandedTask === task.id}
					<div class="px-4 pb-4">
						<div class="border-t pt-3" style="border-color: var(--color-border);">
							<p class="text-xs font-semibold mb-2" style="color: var(--color-text-muted);"
								>Run History</p
							>
							{#if (runs[task.id] ?? []).length === 0}
								<p class="text-xs" style="color: var(--color-text-muted);">No runs yet</p>
							{/if}
							{#each runs[task.id] ?? [] as run}
								<div
									class="flex items-center justify-between py-1.5 text-xs gap-4"
									style="border-bottom: 1px solid var(--color-border);"
								>
									<div class="flex items-center gap-2 min-w-0">
										<span
											class="inline-block w-2 h-2 rounded-full shrink-0"
											style="background-color: {statusColor(run.status)};"
										></span>
										<span class="shrink-0">{run.status}</span>
										<span class="truncate" style="color: var(--color-text-muted);" title={run.details}>
											{run.details?.slice(0, 60) || '-'}
											{#if run.details?.length > 60}...{/if}
										</span>
									</div>
									<span class="shrink-0" style="color: var(--color-text-muted);"
										>{new Date(run.started_at).toLocaleString()}</span
									>
								</div>
							{/each}
						</div>
					</div>
				{/if}
			</div>
		{/each}
		{#if tasks.length === 0 && !showForm}
			<div class="text-center py-12">
				<p class="text-lg mb-2" style="color: var(--color-text-muted);"
					>No maintenance tasks</p
				>
				<p class="text-sm" style="color: var(--color-text-muted);">
					Create scheduled tasks for Docker image prune, DB vacuum, and more
				</p>
			</div>
		{/if}
	</div>
</div>
