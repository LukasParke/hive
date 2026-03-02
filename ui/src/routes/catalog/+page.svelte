<script lang="ts">
	import { api, type TemplateListItem, type TemplateDetail, type Project } from '$lib/api';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';

	let templates = $state<TemplateListItem[]>([]);
	let projects = $state<Project[]>([]);
	let search = $state('');
	let category = $state<string>('all');
	let error = $state('');
	let deploying = $state<string | null>(null);
	let showModal = $state(false);
	let selectedTemplate = $state<TemplateDetail | null>(null);
	let wizardStep = $state(1);
	let selectedProjectId = $state('');
	let envVars = $state<Record<string, string>>({});
	let domain = $state('');
	let volumes = $state<string[]>([]);
	let showImportUrl = $state(false);
	let importUrl = $state('');

	onMount(async () => {
		try {
			[templates, projects] = await Promise.all([
				api.listTemplates(),
				api.listProjects(),
			]);
		} catch (e: any) {
			error = e.message;
		}
	});

	let categories = $derived.by(() => {
		const set = new Set<string>(['all']);
		templates.forEach((t) => set.add(t.category || 'other'));
		return Array.from(set).sort();
	});

	let filtered = $derived(
		templates.filter((t) => {
			const matchSearch =
				t.name.toLowerCase().includes(search.toLowerCase()) ||
				t.description.toLowerCase().includes(search.toLowerCase()) ||
				(t.category || '').toLowerCase().includes(search.toLowerCase());
			const matchCategory = category === 'all' || (t.category || 'other') === category;
			return matchSearch && matchCategory;
		})
	);

	async function openDeploy(template: TemplateListItem) {
		try {
			selectedTemplate = await api.getTemplate(template.name);
			selectedProjectId = projects.length > 0 ? projects[0].id : '';
			envVars = { ...(selectedTemplate.env || {}) };
			domain = selectedTemplate.domain || '';
			volumes = [...(selectedTemplate.volumes || [])];
			wizardStep = 1;
			showModal = true;
		} catch (e: any) {
			error = e.message;
		}
	}

	function nextStep() {
		if (wizardStep < 4) wizardStep++;
	}

	function prevStep() {
		if (wizardStep > 1) wizardStep--;
	}

	async function handleDeploy() {
		if (!selectedTemplate || !selectedProjectId) return;
		deploying = selectedTemplate.name;
		try {
			const result = await api.deployTemplate(selectedTemplate.name, {
				project_id: selectedProjectId,
				domain: domain || undefined,
				env: Object.keys(envVars).length ? envVars : undefined,
				volumes: volumes.length ? volumes : undefined,
			});
			showModal = false;
			if ('id' in result) {
				goto(`/projects/${selectedProjectId}/apps/${result.id}`);
			} else if (result.stack) {
				goto(`/projects/${selectedProjectId}/stacks`);
			}
		} catch (e: any) {
			error = e.message;
		} finally {
			deploying = null;
		}
	}

	async function handleImportFromUrl() {
		if (!importUrl.trim()) return;
		error = '';
		try {
		const source = await api.createTemplateSource({
				name: importUrl.split('/').pop()?.replace('.git', '') || 'imported',
				url: importUrl.trim(),
				type: 'git',
			});
			await api.syncTemplateSource(source.id);
			templates = await api.listTemplates();
			showImportUrl = false;
			importUrl = '';
		} catch (e: any) {
			error = e.message;
		}
	}
</script>

<div>
	<div class="flex items-center justify-between mb-6 flex-wrap gap-4">
		<h2 class="text-2xl font-bold">Template Marketplace</h2>
		<div class="flex gap-2">
			<button
				onclick={() => (showImportUrl = !showImportUrl)}
				class="px-3 py-2 rounded-lg text-sm font-medium cursor-pointer"
				style="background-color: var(--color-surface); border: 1px solid var(--color-border); color: var(--color-text);"
			>
				Import from URL
			</button>
		</div>
	</div>

	{#if showImportUrl}
		<div
			class="rounded-lg p-4 mb-6 flex gap-2 items-end"
			style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
		>
			<div class="flex-1">
				<label class="block text-xs mb-1" style="color: var(--color-text-muted);">Git repository URL</label>
				<input
					type="text"
					bind:value={importUrl}
					placeholder="https://github.com/user/repo.git"
					class="w-full px-3 py-2 rounded-lg text-sm outline-none"
					style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
				/>
			</div>
			<button
				onclick={handleImportFromUrl}
				class="px-4 py-2 rounded-lg text-sm font-medium cursor-pointer"
				style="background-color: var(--color-primary); color: var(--color-bg);"
			>
				Import & Sync
			</button>
		</div>
	{/if}

	<input
		type="text"
		bind:value={search}
		placeholder="Search templates..."
		class="w-full max-w-md px-3 py-2 rounded-lg text-sm outline-none mb-4"
		style="background-color: var(--color-surface); border: 1px solid var(--color-border); color: var(--color-text);"
	/>

	<div class="flex gap-2 mb-4 flex-wrap">
		{#each categories as cat}
			<button
				onclick={() => (category = cat)}
				class="px-3 py-1.5 rounded-lg text-sm cursor-pointer"
				style="background-color: {category === cat ? 'var(--color-primary)' : 'var(--color-surface)'}; color: {category === cat ? 'var(--color-bg)' : 'var(--color-text)'}; border: 1px solid var(--color-border);"
			>
				{cat === 'all' ? 'All' : cat}
			</button>
		{/each}
	</div>

	{#if error}
		<div
			class="rounded-lg p-4 mb-4"
			style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);"
		>
			<p style="color: var(--color-danger);">{error}</p>
		</div>
	{/if}

	<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
		{#each filtered as template}
			<div
				class="rounded-lg p-4 cursor-pointer transition-shadow hover:shadow-md"
				style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
				onclick={() => openDeploy(template)}
			>
				<div class="flex items-start gap-3">
					{#if template.icon}
						<img src={template.icon} alt="" class="w-10 h-10 rounded object-contain" />
					{:else}
						<div
							class="w-10 h-10 rounded flex items-center justify-center text-lg shrink-0"
							style="background-color: var(--color-bg);"
						>
							{template.name[0]?.toUpperCase() || '?'}
						</div>
					{/if}
					<div class="flex-1 min-w-0">
						<div class="flex items-center gap-2">
							<h3 class="font-semibold truncate">{template.name}</h3>
							{#if template.source === 'custom'}
								<span
									class="text-[10px] px-1.5 py-0.5 rounded shrink-0"
									style="background-color: var(--color-bg); color: var(--color-text-muted);"
								>
									Custom
								</span>
							{/if}
						</div>
						<p class="text-sm mt-0.5 line-clamp-2" style="color: var(--color-text-muted);">
							{template.description}
						</p>
						<span
							class="inline-block text-xs mt-2 px-2 py-0.5 rounded"
							style="background-color: var(--color-bg); color: var(--color-text-muted);"
						>
							{template.category || 'other'}
						</span>
					</div>
				</div>
				<button
					onclick={(e) => {
						e.stopPropagation();
						openDeploy(template);
					}}
					disabled={deploying === template.name}
					class="mt-3 w-full py-2 rounded-lg text-sm font-medium cursor-pointer disabled:opacity-50"
					style="background-color: var(--color-primary); color: var(--color-bg);"
				>
					{deploying === template.name ? 'Deploying...' : 'Deploy'}
				</button>
			</div>
		{/each}
	</div>

	{#if filtered.length === 0 && !error}
		<div class="text-center py-12" style="color: var(--color-text-muted);">
			<p class="text-lg mb-2">No templates found</p>
			<p class="text-sm">Try adjusting your search or category filter.</p>
		</div>
	{/if}
</div>

{#if showModal && selectedTemplate}
	<div
		class="fixed inset-0 z-50 flex items-center justify-center p-4"
		style="background-color: rgba(0,0,0,0.5);"
	>
		<div
			class="rounded-lg p-6 w-full max-w-lg max-h-[90vh] overflow-y-auto"
			style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
		>
			<h3 class="text-lg font-bold mb-4">Deploy {selectedTemplate.name}</h3>

			<!-- Step indicator -->
			<div class="flex gap-2 mb-6">
				{#each [1, 2, 3, 4] as s}
					<div
						class="w-8 h-8 rounded-full flex items-center justify-center text-sm font-medium"
						style="background-color: {wizardStep >= s ? 'var(--color-primary)' : 'var(--color-bg)'}; color: {wizardStep >= s ? 'var(--color-bg)' : 'var(--color-text-muted)'};"
					>
						{s}
					</div>
				{/each}
			</div>

			{#if wizardStep === 1}
				<label class="block text-sm mb-2" style="color: var(--color-text-muted);">Select Project</label>
				<select
					bind:value={selectedProjectId}
					class="w-full px-3 py-2 rounded-lg text-sm mb-4"
					style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
				>
					{#each projects as project}
						<option value={project.id}>{project.name}</option>
					{/each}
				</select>
				{#if projects.length === 0}
					<p class="text-sm mb-4" style="color: var(--color-danger);">Create a project first.</p>
				{/if}
			{:else if wizardStep === 2}
				<label class="block text-sm mb-2" style="color: var(--color-text-muted);">Environment Variables</label>
				<div class="space-y-2 mb-4">
					{#each Object.entries(envVars) as [key, val]}
						<div class="flex gap-2">
							<input
								type="text"
								value={key}
								readonly
								class="flex-1 px-3 py-2 rounded-lg text-sm"
								style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text-muted);"
							/>
							<input
								type="text"
								bind:value={envVars[key]}
								class="flex-1 px-3 py-2 rounded-lg text-sm"
								style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
							/>
						</div>
					{/each}
					{#if Object.keys(envVars).length === 0}
						<p class="text-sm" style="color: var(--color-text-muted);">No environment variables for this template.</p>
					{/if}
				</div>
			{:else if wizardStep === 3}
				<label class="block text-sm mb-2" style="color: var(--color-text-muted);">Domain (optional)</label>
				<input
					type="text"
					bind:value={domain}
					placeholder="app.example.com"
					class="w-full px-3 py-2 rounded-lg text-sm mb-4"
					style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
				/>
				<label class="block text-sm mb-2" style="color: var(--color-text-muted);">Volumes (optional, one per line)</label>
				<textarea
					value={volumes.join('\n')}
					oninput={(e) => (volumes = (e.target as HTMLTextAreaElement).value.split('\n').map((s) => s.trim()).filter(Boolean))}
					placeholder="data:/data"
					rows="3"
					class="w-full px-3 py-2 rounded-lg text-sm mb-4 font-mono"
					style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
				></textarea>
			{:else}
				<div class="mb-4 space-y-2">
					<p><strong>Project:</strong> {projects.find((p) => p.id === selectedProjectId)?.name || selectedProjectId}</p>
					<p><strong>Domain:</strong> {domain || '(none)'}</p>
					{#if Object.keys(envVars).length}
						<p><strong>Env vars:</strong> {Object.keys(envVars).length} configured</p>
					{/if}
				</div>
			{/if}

			<div class="flex gap-2 justify-end mt-6">
				{#if wizardStep > 1}
					<button
						onclick={prevStep}
						class="px-4 py-2 rounded-lg text-sm cursor-pointer"
						style="color: var(--color-text-muted);"
					>
						Back
					</button>
				{/if}
				<button
					onclick={() => (showModal = false)}
					class="px-4 py-2 rounded-lg text-sm cursor-pointer"
					style="color: var(--color-text-muted);"
				>
					Cancel
				</button>
				{#if wizardStep < 4}
					<button
						onclick={nextStep}
						disabled={wizardStep === 1 && !selectedProjectId}
						class="px-4 py-2 rounded-lg text-sm font-medium cursor-pointer disabled:opacity-50"
						style="background-color: var(--color-primary); color: var(--color-bg);"
					>
						Next
					</button>
				{:else}
					<button
						onclick={handleDeploy}
						disabled={!selectedProjectId || deploying !== null}
						class="px-4 py-2 rounded-lg text-sm font-medium cursor-pointer disabled:opacity-50"
						style="background-color: var(--color-primary); color: var(--color-bg);"
					>
						{deploying ? 'Deploying...' : 'Deploy'}
					</button>
				{/if}
			</div>
		</div>
	</div>
{/if}
