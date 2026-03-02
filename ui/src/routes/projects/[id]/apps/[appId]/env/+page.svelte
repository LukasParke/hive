<script lang="ts">
	import { page } from '$app/stores';
	import { api, type AppEnvVar, type App } from '$lib/api';
	import { onMount } from 'svelte';

	let envVars = $state<AppEnvVar[]>([]);
	let app = $state<App | null>(null);
	let error = $state('');
	let loading = $state(true);
	let showAdd = $state(false);
	let showImport = $state(false);
	let editingKey = $state<string | null>(null);
	let deleteConfirmKey = $state<string | null>(null);

	const projectId = $derived(($page.params as { id?: string }).id ?? '');
	const appId = $derived(($page.params as { appId?: string }).appId ?? '');

	let newVar = $state({ key: '', value: '', is_secret: false });
	let importContent = $state('');
	let editValue = $state('');

	onMount(() => loadData());

	async function loadData() {
		try {
			[app, envVars] = await Promise.all([
				api.getApp(projectId, appId),
				api.listEnvVars(projectId, appId),
			]);
			error = '';
		} catch (e: any) {
			error = e.message;
		} finally {
			loading = false;
		}
	}

	async function addVar(e: Event) {
		e.preventDefault();
		if (!newVar.key.trim()) return;
		try {
			const created = await api.setEnvVar(projectId, appId, {
				key: newVar.key.trim(),
				value: newVar.value,
				is_secret: newVar.is_secret,
			});
			envVars = [created, ...envVars.filter((v) => v.key !== created.key)];
			showAdd = false;
			newVar = { key: '', value: '', is_secret: false };
			error = '';
		} catch (e: any) {
			error = e.message;
		}
	}

	function startEdit(ev: AppEnvVar) {
		editingKey = ev.key;
		editValue = ev.value;
	}

	async function saveEdit(key: string) {
		try {
			const updated = await api.setEnvVar(projectId, appId, {
				key,
				value: editValue,
				is_secret: envVars.find((v) => v.key === key)?.is_secret ?? false,
			});
			envVars = envVars.map((v) => (v.key === key ? updated : v));
			editingKey = null;
			error = '';
		} catch (e: any) {
			error = e.message;
		}
	}

	function cancelEdit() {
		editingKey = null;
	}

	async function deleteVar(key: string) {
		if (deleteConfirmKey !== key) {
			deleteConfirmKey = key;
			return;
		}
		try {
			await api.deleteEnvVar(projectId, appId, key);
			envVars = envVars.filter((v) => v.key !== key);
			deleteConfirmKey = null;
			error = '';
		} catch (e: any) {
			error = e.message;
		}
	}

	function clearDeleteConfirm() {
		deleteConfirmKey = null;
	}

	async function importVars(e: Event) {
		e.preventDefault();
		try {
			const { imported } = await api.importEnvVars(projectId, appId, importContent);
			showImport = false;
			importContent = '';
			await loadData();
			error = '';
		} catch (e: any) {
			error = e.message;
		}
	}

	async function exportVars() {
		try {
			const content = await api.exportEnvVars(projectId, appId);
			const blob = new Blob([content], { type: 'text/plain' });
			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = '.env';
			a.click();
			URL.revokeObjectURL(url);
		} catch (e: any) {
			error = e.message;
		}
	}

	function displayValue(ev: AppEnvVar): string {
		return ev.is_secret ? ev.value : ev.value;
	}
</script>

<div>
	<div class="mb-6">
		<a href="/projects/{projectId}" class="text-sm" style="color: var(--color-text-muted);">Projects</a>
		<span class="text-sm" style="color: var(--color-text-muted);"> / </span>
		<a href="/apps/{appId}?project={projectId}" class="text-sm" style="color: var(--color-text-muted);">{app?.name ?? 'App'}</a>
		<span class="text-sm" style="color: var(--color-text-muted);"> / </span>
		<span class="text-sm font-medium" style="color: var(--color-text);">Environment Variables</span>
		<h2 class="text-2xl font-bold mt-1">Environment Variables</h2>
		<p class="text-sm mt-1" style="color: var(--color-text-muted);">
			Manage environment variables for this app. Values are stored encrypted. Secrets are masked in the UI and exports.
		</p>
	</div>

	{#if loading}
		<div class="flex items-center justify-center py-12">
			<div class="animate-spin rounded-full h-8 w-8 border-b-2" style="border-color: var(--color-primary);"></div>
		</div>
	{:else}
		<div class="flex items-center justify-between mb-4 gap-2 flex-wrap">
			<span class="text-sm" style="color: var(--color-text-muted);">{envVars.length} variable{envVars.length !== 1 ? 's' : ''}</span>
			<div class="flex gap-2">
				<button
					onclick={() => { showImport = true; showAdd = false; }}
					class="px-3 py-1.5 rounded-lg text-sm font-medium cursor-pointer"
					style="border: 1px solid var(--color-border); color: var(--color-text);"
				>
					Import .env
				</button>
				<button
					onclick={exportVars}
					class="px-3 py-1.5 rounded-lg text-sm font-medium cursor-pointer"
					style="border: 1px solid var(--color-border); color: var(--color-text);"
				>
					Export
				</button>
				<button
					onclick={() => { showAdd = true; showImport = false; }}
					class="px-3 py-1.5 rounded-lg text-sm font-medium cursor-pointer"
					style="background-color: var(--color-primary); color: var(--color-bg);"
				>
					Add Variable
				</button>
			</div>
		</div>

		{#if showAdd}
			<form onsubmit={addVar} class="rounded-lg p-4 mb-6 space-y-3" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<div class="grid grid-cols-1 md:grid-cols-2 gap-3">
					<input
						type="text"
						bind:value={newVar.key}
						placeholder="KEY (e.g. DATABASE_URL)"
						required
						class="w-full px-3 py-2 rounded-lg text-sm outline-none font-mono"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
					/>
					<input
						type={newVar.is_secret ? 'password' : 'text'}
						bind:value={newVar.value}
						placeholder="Value"
						required
						class="w-full px-3 py-2 rounded-lg text-sm outline-none font-mono"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
					/>
				</div>
				<label class="flex items-center gap-2 cursor-pointer">
					<input type="checkbox" bind:checked={newVar.is_secret} class="rounded" />
					<span class="text-sm" style="color: var(--color-text-muted);">Secret (value will be masked)</span>
				</label>
				<div class="flex gap-2">
					<button type="submit" class="px-4 py-2 rounded-lg text-sm font-medium cursor-pointer" style="background-color: var(--color-primary); color: var(--color-bg);">
						Add
					</button>
					<button type="button" onclick={() => showAdd = false} class="px-4 py-2 rounded-lg text-sm cursor-pointer" style="color: var(--color-text-muted);">
						Cancel
					</button>
				</div>
			</form>
		{/if}

		{#if showImport}
			<form onsubmit={importVars} class="rounded-lg p-4 mb-6 space-y-3" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<label for="import-content" class="block text-sm font-medium mb-1" style="color: var(--color-text-muted);">Paste .env content (KEY=value format)</label>
				<textarea
					bind:value={importContent}
					placeholder="DATABASE_URL=postgres://...
API_KEY=secret123
NODE_ENV=production"
					rows="8"
					class="w-full px-3 py-2 rounded-lg text-sm outline-none font-mono resize-y"
					style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
				></textarea>
				<p class="text-xs" style="color: var(--color-text-muted);">Existing keys will be updated. New keys will be added.</p>
				<div class="flex gap-2">
					<button type="submit" class="px-4 py-2 rounded-lg text-sm font-medium cursor-pointer" style="background-color: var(--color-primary); color: var(--color-bg);">
						Import
					</button>
					<button type="button" onclick={() => { showImport = false; importContent = ''; }} class="px-4 py-2 rounded-lg text-sm cursor-pointer" style="color: var(--color-text-muted);">
						Cancel
					</button>
				</div>
			</form>
		{/if}

		<div class="rounded-lg overflow-hidden" style="border: 1px solid var(--color-border);">
			<table class="w-full text-sm">
				<thead>
					<tr style="background-color: var(--color-surface); border-bottom: 1px solid var(--color-border);">
						<th class="text-left px-4 py-3 font-semibold" style="color: var(--color-text);">Key</th>
						<th class="text-left px-4 py-3 font-semibold" style="color: var(--color-text);">Value</th>
						<th class="text-left px-4 py-3 font-semibold w-24" style="color: var(--color-text);">Source</th>
						<th class="text-right px-4 py-3 font-semibold w-32" style="color: var(--color-text);">Actions</th>
					</tr>
				</thead>
				<tbody>
					{#each envVars as ev}
						<tr style="border-bottom: 1px solid var(--color-border);">
							<td class="px-4 py-3 font-mono" style="color: var(--color-text);">{ev.key}</td>
							<td class="px-4 py-3">
								{#if editingKey === ev.key}
									<div class="flex items-center gap-2">
										<input
											type={ev.is_secret ? 'password' : 'text'}
											bind:value={editValue}
											class="flex-1 px-2 py-1 rounded text-sm font-mono"
											style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
										/>
										<button
											onclick={() => saveEdit(ev.key)}
											class="px-2 py-1 rounded text-xs cursor-pointer"
											style="background-color: var(--color-primary); color: var(--color-bg);"
										>
											Save
										</button>
										<button
											onclick={cancelEdit}
											class="px-2 py-1 rounded text-xs cursor-pointer"
											style="color: var(--color-text-muted);"
										>
											Cancel
										</button>
									</div>
								{:else}
									<span class="font-mono" style="color: var(--color-text-muted);">
										{ev.is_secret ? ev.value : ev.value}
									</span>
								{/if}
							</td>
							<td class="px-4 py-3 text-xs" style="color: var(--color-text-muted);">{ev.source}</td>
							<td class="px-4 py-3 text-right">
								{#if editingKey !== ev.key}
									<button
										onclick={() => startEdit(ev)}
										class="px-2 py-1 rounded text-xs cursor-pointer mr-1"
										style="color: var(--color-primary);"
									>
										Edit
									</button>
									<button
										onclick={() => deleteVar(ev.key)}
										class="px-2 py-1 rounded text-xs cursor-pointer"
										style="color: var(--color-danger);"
									>
										{deleteConfirmKey === ev.key ? 'Confirm?' : 'Delete'}
									</button>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
			{#if envVars.length === 0}
				<div class="px-4 py-8 text-center text-sm" style="color: var(--color-text-muted);">No environment variables yet. Add one or import from a .env file.</div>
			{/if}
		</div>
	{/if}

	{#if error}
		<div class="rounded-lg p-4 mt-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);">
			<p style="color: var(--color-danger);">{error}</p>
		</div>
	{/if}
</div>
