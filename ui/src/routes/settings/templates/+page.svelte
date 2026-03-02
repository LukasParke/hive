<script lang="ts">
	import { api, type TemplateSource, type CustomTemplate } from '$lib/api';
	import { onMount } from 'svelte';

	let sources = $state<TemplateSource[]>([]);
	let customTemplates = $state<CustomTemplate[]>([]);
	let error = $state('');
	let showAddSource = $state(false);
	let newName = $state('');
	let newUrl = $state('');
	let syncing = $state<string | null>(null);
	let deleting = $state<string | null>(null);

	onMount(load);

	async function load() {
		try {
			[sources, customTemplates] = await Promise.all([
				api.listTemplateSources(),
				api.listCustomTemplates(),
			]);
		} catch (e: any) {
			error = e.message;
		}
	}

	async function addSource() {
		if (!newName.trim() || !newUrl.trim()) {
			error = 'Name and URL are required';
			return;
		}
		error = '';
		try {
			await api.createTemplateSource({ name: newName.trim(), url: newUrl.trim(), type: 'git' });
			showAddSource = false;
			newName = '';
			newUrl = '';
			await load();
		} catch (e: any) {
			error = e.message;
		}
	}

	async function syncSource(source: TemplateSource) {
		syncing = source.id;
		error = '';
		try {
			await api.syncTemplateSource(source.id);
			await load();
		} catch (e: any) {
			error = e.message;
		} finally {
			syncing = null;
		}
	}

	async function deleteSource(source: TemplateSource) {
		if (!confirm(`Remove template source "${source.name}"?`)) return;
		deleting = source.id;
		error = '';
		try {
			await api.deleteTemplateSource(source.id);
			await load();
		} catch (e: any) {
			error = e.message;
		} finally {
			deleting = null;
		}
	}

	async function deleteTemplate(ct: CustomTemplate) {
		if (!confirm(`Delete custom template "${ct.name}"?`)) return;
		deleting = ct.id;
		error = '';
		try {
			await api.deleteCustomTemplate(ct.id);
			await load();
		} catch (e: any) {
			error = e.message;
		} finally {
			deleting = null;
		}
	}

	function formatDate(s: string | null) {
		if (!s) return 'Never';
		try {
			return new Date(s).toLocaleString();
		} catch {
			return s;
		}
	}
</script>

<div class="max-w-4xl mx-auto">
	<h2 class="text-2xl font-bold mb-6">Template Sources & Custom Templates</h2>

	{#if error}
		<div
			class="rounded-lg p-4 mb-6"
			style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);"
		>
			<p style="color: var(--color-danger);">{error}</p>
		</div>
	{/if}

	<!-- Template Sources -->
	<div class="rounded-lg p-4 mb-8" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
		<h3 class="font-semibold mb-4">Template Sources</h3>
		<p class="text-sm mb-4" style="color: var(--color-text-muted);">
			Add git repositories to import YAML templates. Each sync pulls templates from the repo.
		</p>

		{#if showAddSource}
			<div class="rounded-lg p-4 mb-4" style="background-color: var(--color-bg); border: 1px solid var(--color-border);">
				<label class="block text-sm mb-2" style="color: var(--color-text-muted);">Name</label>
				<input
					type="text"
					bind:value={newName}
					placeholder="My Templates"
					class="w-full px-3 py-2 rounded-lg text-sm mb-4"
					style="background-color: var(--color-surface); border: 1px solid var(--color-border); color: var(--color-text);"
				/>
				<label class="block text-sm mb-2" style="color: var(--color-text-muted);">Git URL</label>
				<input
					type="text"
					bind:value={newUrl}
					placeholder="https://github.com/user/repo.git"
					class="w-full px-3 py-2 rounded-lg text-sm mb-4"
					style="background-color: var(--color-surface); border: 1px solid var(--color-border); color: var(--color-text);"
				/>
				<div class="flex gap-2">
					<button
						onclick={addSource}
						class="px-4 py-2 rounded-lg text-sm font-medium cursor-pointer"
						style="background-color: var(--color-primary); color: var(--color-bg);"
					>
						Add
					</button>
					<button
						onclick={() => (showAddSource = false)}
						class="px-4 py-2 rounded-lg text-sm cursor-pointer"
						style="color: var(--color-text-muted);"
					>
						Cancel
					</button>
				</div>
			</div>
		{:else}
			<button
				onclick={() => (showAddSource = true)}
				class="mb-4 px-3 py-2 rounded-lg text-sm font-medium cursor-pointer"
				style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
			>
				+ Add Source
			</button>
		{/if}

		<div class="space-y-3">
			{#each sources as source}
				<div
					class="flex items-center justify-between p-3 rounded-lg"
					style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
				>
					<div>
						<p class="font-medium">{source.name}</p>
						<p class="text-sm" style="color: var(--color-text-muted);">{source.url}</p>
						<p class="text-xs mt-1" style="color: var(--color-text-muted);">
							Last synced: {formatDate(source.last_synced_at)}
						</p>
					</div>
					<div class="flex gap-2">
						<button
							onclick={() => syncSource(source)}
							disabled={syncing !== null}
							class="px-3 py-1.5 rounded-lg text-sm cursor-pointer disabled:opacity-50"
							style="background-color: var(--color-primary); color: var(--color-bg);"
						>
							{syncing === source.id ? 'Syncing...' : 'Sync'}
						</button>
						<button
							onclick={() => deleteSource(source)}
							disabled={deleting !== null}
							class="px-3 py-1.5 rounded-lg text-sm cursor-pointer disabled:opacity-50"
							style="color: var(--color-danger); border: 1px solid var(--color-danger);"
						>
							Delete
						</button>
					</div>
				</div>
			{/each}
			{#if sources.length === 0}
				<p class="text-sm py-4" style="color: var(--color-text-muted);">No template sources yet. Add one to import community templates.</p>
			{/if}
		</div>
	</div>

	<!-- Custom Templates -->
	<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
		<h3 class="font-semibold mb-4">Custom Templates</h3>
		<p class="text-sm mb-4" style="color: var(--color-text-muted);">
			Templates imported from sources or exported from apps. Manage and delete them here.
		</p>

		<div class="space-y-3">
			{#each customTemplates as ct}
				<div
					class="flex items-center justify-between p-3 rounded-lg"
					style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
				>
					<div>
						<p class="font-medium">{ct.name}</p>
						<p class="text-sm line-clamp-1" style="color: var(--color-text-muted);">{ct.description}</p>
						<p class="text-xs mt-1" style="color: var(--color-text-muted);">
							{ct.image} · {ct.category} · v{ct.version}
						</p>
					</div>
					<button
						onclick={() => deleteTemplate(ct)}
						disabled={deleting !== null}
						class="px-3 py-1.5 rounded-lg text-sm cursor-pointer disabled:opacity-50"
						style="color: var(--color-danger); border: 1px solid var(--color-danger);"
					>
						Delete
					</button>
				</div>
			{/each}
			{#if customTemplates.length === 0}
				<p class="text-sm py-4" style="color: var(--color-text-muted);">No custom templates. Sync a source or export an app as template.</p>
			{/if}
		</div>
	</div>
</div>
