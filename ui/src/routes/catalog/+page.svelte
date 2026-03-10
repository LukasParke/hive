<script lang="ts">
	import { api } from '$lib/api';
	import type { TemplateListItem, TemplateDetail } from '$lib/types';
	import { Button, EmptyState } from '$lib/components';
	import { goto, invalidateAll } from '$app/navigation';
	import { toast } from 'svelte-sonner';

	let { data } = $props();

	let search = $state('');
	let selectedCategory = $state<string>('all');
	let sortBy = $state<'name' | 'category'>('name');
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
	let creatingProject = $state(false);
	let creatingProjectLoading = $state(false);
	let importingTemplates = $state(false);
	let newProjectName = $state('');
	let dbStorageMode = $state('local');
	let storageHostId = $state('');
	let dbNodeId = $state('');

	let templates = $derived(data.templates ?? []);

	let categoryCounts = $derived.by(() => {
		const counts: Record<string, number> = { all: templates.length };
		templates.forEach((t: TemplateListItem) => {
			const cat = t.category || 'other';
			counts[cat] = (counts[cat] || 0) + 1;
		});
		return counts;
	});

	let categories = $derived.by(() => {
		const cats = Object.keys(categoryCounts).filter((c) => c !== 'all');
		cats.sort();
		return ['all', ...cats];
	});

	let filtered = $derived.by(() => {
		const q = search.toLowerCase();
		let result = templates.filter((t: TemplateListItem) => {
			const matchSearch =
				!q ||
				t.name.toLowerCase().includes(q) ||
				(t.description || '').toLowerCase().includes(q) ||
				(t.category || '').toLowerCase().includes(q) ||
				(t.tags || []).some((tag: string) => tag.toLowerCase().includes(q));
			const matchCategory =
				selectedCategory === 'all' || (t.category || 'other') === selectedCategory;
			return matchSearch && matchCategory;
		});

		if (sortBy === 'name') {
			result.sort((a: TemplateListItem, b: TemplateListItem) => a.name.localeCompare(b.name));
		} else {
			result.sort((a: TemplateListItem, b: TemplateListItem) =>
				(a.category || 'other').localeCompare(b.category || 'other') || a.name.localeCompare(b.name)
			);
		}

		return result;
	});

	let hasRequiredEnv = $derived(
		selectedTemplate ? Object.keys(selectedTemplate.env || {}).length > 0 : false
	);
	let totalSteps = $derived(hasRequiredEnv ? 3 : 2);

	async function quickDeploy(template: TemplateListItem) {
		deploying = template.name;
		error = '';
		try {
			const result = await api.deployTemplate(template.name, {
				project_id: selectedProjectId || undefined,
			});
			toast.success(`Deploy started for ${template.name}`);
			if ('id' in result) {
				const pid = result.project_id || selectedProjectId;
				if (pid) goto(`/projects/${pid}/apps/${result.id}`);
				else goto('/projects');
			} else if ((result as any).stack) {
				const pid = selectedProjectId || (result as any).project_id;
				if (pid) goto(`/projects/${pid}/stacks`);
				else goto('/projects');
			}
		} catch (e: any) {
			error = e.message;
			toast.error(e.message ?? `Failed to deploy ${template.name}`);
		} finally {
			deploying = null;
		}
	}

	async function openDeploy(template: TemplateListItem) {
		try {
			selectedTemplate = await api.getTemplate(template.name);
			selectedProjectId =
				(data.projects ?? []).length > 0 ? ((data.projects ?? [])[0]?.id ?? '') : '';
			envVars = { ...(selectedTemplate.env || {}) };
			domain = selectedTemplate.domain || '';
			volumes = [...(selectedTemplate.volumes || [])];
			wizardStep = 1;
			creatingProject = false;
			newProjectName = '';
			showModal = true;
		} catch (e: any) {
			error = e.message;
		}
	}

	function nextStep() {
		if (wizardStep < totalSteps) wizardStep++;
	}
	function prevStep() {
		if (wizardStep > 1) wizardStep--;
	}

	async function createProjectInline() {
		if (!newProjectName.trim()) return;
		creatingProjectLoading = true;
		try {
			const project = await api.createProject({ name: newProjectName.trim() });
			await invalidateAll();
			selectedProjectId = project.id;
			creatingProject = false;
			newProjectName = '';
			toast.success('Project created.');
		} catch (e: any) {
			error = e.message;
		} finally {
			creatingProjectLoading = false;
		}
	}

	async function handleDeploy() {
		if (!selectedTemplate) return;
		if (!selectedProjectId && (data.projects ?? []).length > 0) {
			error = 'Select a project or create one before deploying.';
			return;
		}
		if (domain && !/^(\*\.)?[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/.test(domain.trim())) {
			error = 'Domain must be valid (example.com or *.example.com).';
			return;
		}
		if (selectedTemplate?.env) {
			for (const [k, v] of Object.entries(selectedTemplate.env)) {
				if (String(v ?? '').trim() === '' && String(envVars[k] ?? '').trim() === '') {
					error = `Environment variable ${k} is required.`;
					return;
				}
			}
		}
		deploying = selectedTemplate.name;
		try {
			const result = await api.deployTemplate(selectedTemplate.name, {
				project_id: selectedProjectId || undefined,
				domain: domain || undefined,
				env: Object.keys(envVars).length ? envVars : undefined,
				volumes: volumes.length ? volumes : undefined,
				storage_host_id: storageHostId || undefined,
				db_storage_mode: dbStorageMode !== 'local' ? dbStorageMode : undefined,
				db_node_id: dbNodeId || undefined,
			});
			toast.success(`Deploy started for ${selectedTemplate.name}`);
			showModal = false;
			if ('id' in result) {
				const pid = result.project_id || selectedProjectId;
				if (pid) goto(`/projects/${pid}/apps/${result.id}`);
				else goto('/projects');
			} else if ((result as any).stack) {
				const pid = selectedProjectId || (result as any).project_id;
				if (pid) goto(`/projects/${pid}/stacks`);
				else goto('/projects');
			}
		} catch (e: any) {
			error = e.message;
			toast.error(e.message ?? `Failed to deploy ${selectedTemplate.name}`);
		} finally {
			deploying = null;
		}
	}

	async function handleImportFromUrl() {
		if (!importUrl.trim()) return;
		importingTemplates = true;
		error = '';
		try {
			const source = await api.createTemplateSource({
				name: importUrl.split('/').pop()?.replace('.git', '') || 'imported',
				url: importUrl.trim(),
				type: 'git',
			});
			await api.syncTemplateSource(source.id);
			await invalidateAll();
			showImportUrl = false;
			importUrl = '';
			toast.success('Template source imported and synced.');
		} catch (e: any) {
			error = e.message;
		} finally {
			importingTemplates = false;
		}
	}

	function formatCategory(cat: string): string {
		return cat
			.split('-')
			.map((w) => w.charAt(0).toUpperCase() + w.slice(1))
			.join(' ');
	}
</script>

<svelte:head><title>Catalog | Hive</title></svelte:head>

<div class="catalog-page">
	<div class="page-header">
		<div>
			<h2 class="page-title">App Catalog</h2>
			<p class="page-subtitle">
				{filtered.length} of {templates.length} template{templates.length !== 1 ? 's' : ''}
			</p>
		</div>
		<div class="flex gap-2 flex-wrap">
			<Button variant="secondary" onclick={() => (showImportUrl = !showImportUrl)}>
				<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
				Import
			</Button>
		</div>
	</div>

	{#if showImportUrl}
		<div class="import-bar">
			<div class="flex-1">
				<label class="block text-xs mb-1" style="color: var(--color-text-muted);">Template source URL (raw YAML)</label>
				<input type="text" bind:value={importUrl} placeholder="https://example.com/templates.yaml" class="import-input" />
			</div>
			<button onclick={handleImportFromUrl} class="import-btn" disabled={importingTemplates}>
				{importingTemplates ? 'Importing...' : 'Import & Sync'}
			</button>
		</div>
	{/if}

	<div class="controls-row">
		<div class="search-box">
			<svg class="search-icon" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
				<circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>
			</svg>
			<input type="text" bind:value={search} placeholder="Search templates..." class="search-input" />
			{#if search}
				<button onclick={() => search = ''} class="search-clear">&times;</button>
			{/if}
		</div>
		<div class="sort-toggle">
			<button class="sort-btn" class:active={sortBy === 'name'} onclick={() => sortBy = 'name'}>A-Z</button>
			<button class="sort-btn" class:active={sortBy === 'category'} onclick={() => sortBy = 'category'}>Category</button>
		</div>
	</div>

	<div class="category-chips">
		{#each categories as cat}
			<button
				onclick={() => (selectedCategory = cat)}
				class="chip"
				class:active={selectedCategory === cat}
			>
				{cat === 'all' ? 'All' : formatCategory(cat)}
				<span class="chip-count">{categoryCounts[cat] || 0}</span>
			</button>
		{/each}
	</div>

	{#if error}
		<div class="error-banner">
			<p>{error}</p>
			<button onclick={() => error = ''} class="error-dismiss">&times;</button>
		</div>
	{/if}

	<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-3">
		{#each filtered as template (template.name)}
			<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
			<div class="card" onclick={() => openDeploy(template)}>
				<div class="card-header">
					{#if template.icon}
						<img
							src={template.icon}
							alt=""
							class="card-icon"
							onerror={(e) => { (e.currentTarget as HTMLImageElement).style.display = 'none'; (e.currentTarget as HTMLImageElement).nextElementSibling?.classList.remove('hidden'); }}
						/>
						<div class="card-icon-fallback hidden">{template.name[0]?.toUpperCase() || '?'}</div>
					{:else}
						<div class="card-icon-fallback">{template.name[0]?.toUpperCase() || '?'}</div>
					{/if}
					<div class="card-title-group">
						<h3 class="card-title">{template.name}</h3>
						<div class="card-badges">
							{#if template.version && template.version !== 'latest'}
								<span class="badge badge-version">{template.version}</span>
							{/if}
							{#if template.is_stack}
								<span class="badge badge-stack">Stack</span>
							{/if}
						</div>
					</div>
				</div>

				<p class="card-desc">{template.description || 'No description'}</p>

				<div class="card-meta">
					<span class="card-category">{formatCategory(template.category || 'other')}</span>
					{#if template.links}
						<div class="card-links">
							{#if template.links.github}
								<a href={template.links.github} target="_blank" rel="noopener" onclick={(e) => e.stopPropagation()} title="GitHub">
									<svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor"><path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/></svg>
								</a>
							{/if}
							{#if template.links.website}
								<a href={template.links.website} target="_blank" rel="noopener" onclick={(e) => e.stopPropagation()} title="Website">
									<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>
								</a>
							{/if}
							{#if template.links.docs}
								<a href={template.links.docs} target="_blank" rel="noopener" onclick={(e) => e.stopPropagation()} title="Docs">
									<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/></svg>
								</a>
							{/if}
						</div>
					{/if}
				</div>

				<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
				<div class="card-actions" onclick={(e) => e.stopPropagation()}>
					<button onclick={() => openDeploy(template)} class="btn-configure">Configure</button>
					<button
						onclick={() => quickDeploy(template)}
						disabled={deploying === template.name}
						class="btn-deploy"
					>
						{deploying === template.name ? 'Deploying...' : 'Deploy'}
					</button>
				</div>
			</div>
		{/each}
	</div>

	{#if filtered.length === 0 && !error}
		<EmptyState title="No templates found" description="Try adjusting your search or category filter." />
	{/if}
</div>

{#if showModal && selectedTemplate}
	<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
	<div class="modal-backdrop" onclick={() => (showModal = false)}>
		<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
		<div class="modal-content" onclick={(e) => e.stopPropagation()}>
			<div class="modal-header">
				{#if selectedTemplate.icon}
					<img src={selectedTemplate.icon} alt="" class="modal-icon" onerror={(e) => { (e.currentTarget as HTMLImageElement).style.display = 'none'; }} />
				{/if}
				<div>
					<h3 class="modal-title">Deploy {selectedTemplate.name}</h3>
					<p class="modal-subtitle">{selectedTemplate.description}</p>
				</div>
			</div>

			{#if selectedTemplate.links && Object.keys(selectedTemplate.links).length > 0}
				<div class="modal-links">
					{#each Object.entries(selectedTemplate.links) as [key, url]}
						{#if url}
							<a href={url} target="_blank" rel="noopener" class="modal-link">{key}</a>
						{/if}
					{/each}
				</div>
			{/if}

			{#if selectedTemplate.is_stack && selectedTemplate.services?.length}
				<div class="modal-services">
					<p class="modal-services-label">Services ({selectedTemplate.services.length})</p>
					<div class="modal-services-list">
						{#each selectedTemplate.services as svc}
							<span class="modal-service-chip">{svc.name}</span>
						{/each}
					</div>
				</div>
			{/if}

			<div class="modal-steps">
				{#each Array.from({ length: totalSteps }, (_, i) => i + 1) as s}
					<div class="step-dot" class:active={wizardStep >= s}>{s}</div>
				{/each}
			</div>

			{#if wizardStep === 1}
				<div class="space-y-4">
					<div>
						<label class="field-label">Project</label>
						{#if !creatingProject}
							<div class="flex gap-2">
								<select bind:value={selectedProjectId} class="field-input flex-1">
									<option value="">Auto (My Apps)</option>
									{#each data.projects ?? [] as project}
										<option value={project.id}>{project.name}</option>
									{/each}
								</select>
								<button onclick={() => (creatingProject = true)} class="btn-secondary-sm">New</button>
							</div>
						{:else}
							<div class="flex gap-2">
								<input type="text" bind:value={newProjectName} placeholder="Project name..." class="field-input flex-1" />
								<button onclick={createProjectInline} class="btn-primary-sm" disabled={creatingProjectLoading}>
									{creatingProjectLoading ? 'Creating...' : 'Create'}
								</button>
								<button onclick={() => (creatingProject = false)} class="btn-text-sm">Cancel</button>
							</div>
						{/if}
					</div>
					<div>
						<label class="field-label">Domain (optional)</label>
						<input type="text" bind:value={domain} placeholder="app.example.com" class="field-input w-full" />
					</div>
					{#if selectedTemplate.volumes && selectedTemplate.volumes.length > 0}
						<div>
							<label class="field-label">Volumes</label>
							<textarea
								value={volumes.join('\n')}
								oninput={(e) => (volumes = (e.target as HTMLTextAreaElement).value.split('\n').map((s) => s.trim()).filter(Boolean))}
								placeholder="data:/data"
								rows="3"
								class="field-input w-full font-mono"
							></textarea>
						</div>
					{/if}
				</div>
			{:else if wizardStep === 2 && hasRequiredEnv}
				<label class="field-label">Environment Variables</label>
				<div class="space-y-2 mb-4">
					{#each Object.entries(envVars) as [key, val]}
						<div class="flex flex-col sm:flex-row gap-2">
							<input type="text" value={key} readonly class="field-input sm:flex-1 field-muted" />
							<input type="text" bind:value={envVars[key]} class="field-input sm:flex-1" />
						</div>
					{/each}
				</div>
			{:else}
				<div class="summary">
					<div class="summary-row">
						<span class="summary-label">Template</span>
						<span class="summary-value">{selectedTemplate.name}</span>
					</div>
					<div class="summary-row">
						<span class="summary-label">Project</span>
						<span class="summary-value">{(data.projects ?? []).find((p) => p.id === selectedProjectId)?.name || 'My Apps (auto)'}</span>
					</div>
					<div class="summary-row">
						<span class="summary-label">Image</span>
						<span class="summary-value font-mono text-xs">{selectedTemplate.image}</span>
					</div>
					{#if domain}
						<div class="summary-row">
							<span class="summary-label">Domain</span>
							<span class="summary-value">{domain}</span>
						</div>
					{/if}
					{#if Object.keys(envVars).length}
						<div class="summary-row">
							<span class="summary-label">Env vars</span>
							<span class="summary-value">{Object.keys(envVars).length} configured</span>
						</div>
					{/if}
					{#if selectedTemplate.is_stack}
						<div class="summary-row">
							<span class="summary-label">Type</span>
							<span class="summary-value">Stack ({selectedTemplate.services?.length || 0} services)</span>
						</div>
					{/if}
				</div>

				{#if selectedTemplate.depends_on && selectedTemplate.depends_on.length > 0}
					<div class="mt-4 p-3 rounded-lg" style="background-color: var(--color-bg); border: 1px solid var(--color-border);">
						<label class="field-label">Database Storage Mode</label>
						<p class="text-xs mb-2" style="color: var(--color-text-muted);">This template requires: {selectedTemplate.depends_on.map(d => d.type).join(', ')}</p>
						<select bind:value={dbStorageMode} class="field-input w-full mb-2">
							<option value="local">Local (default Docker volume)</option>
							<option value="pinned">Pinned to specific node</option>
							<option value="remote">Remote storage (NFS/Ceph)</option>
							<option value="ha">HA / Distributed</option>
						</select>
					</div>
				{/if}

				{#if (selectedTemplate.nas_volumes && selectedTemplate.nas_volumes.length > 0)}
					<div class="mt-4 p-3 rounded-lg" style="background-color: var(--color-bg); border: 1px solid var(--color-border);">
						<label class="field-label">NAS Volumes</label>
						<p class="text-xs mb-2" style="color: var(--color-text-muted);">Optional: attach NAS storage for media/data volumes</p>
						{#each selectedTemplate.nas_volumes as nv}
							<div class="flex items-center gap-2 text-sm mb-1">
								<span class="font-medium">{nv.name}</span>
								<span class="text-xs" style="color: var(--color-text-muted);">{nv.suggested_path}</span>
								{#if nv.description}
									<span class="text-xs" style="color: var(--color-text-muted);">- {nv.description}</span>
								{/if}
							</div>
						{/each}
					</div>
				{/if}
			{/if}

			<div class="modal-actions">
				{#if wizardStep > 1}
					<button onclick={prevStep} class="btn-text-sm">Back</button>
				{/if}
				<button onclick={() => (showModal = false)} class="btn-text-sm">Cancel</button>
				<div class="flex-1"></div>
				{#if wizardStep < totalSteps}
					<button onclick={nextStep} class="btn-primary-sm">Next</button>
				{:else}
					<button onclick={handleDeploy} disabled={deploying !== null} class="btn-primary-sm">
						{deploying ? 'Deploying...' : 'Deploy'}
					</button>
				{/if}
			</div>
		</div>
	</div>
{/if}

<style>
	.catalog-page {
		max-width: 100%;
	}

	.controls-row {
		display: flex;
		gap: 0.75rem;
		margin-bottom: 0.75rem;
		flex-wrap: wrap;
		align-items: center;
	}

	.search-box {
		position: relative;
		flex: 1;
		min-width: 200px;
		max-width: 400px;
	}
	.search-icon {
		position: absolute;
		left: 0.75rem;
		top: 50%;
		transform: translateY(-50%);
		color: var(--color-text-muted);
		pointer-events: none;
	}
	.search-input {
		width: 100%;
		padding: 0.5rem 2rem 0.5rem 2.25rem;
		border-radius: 0.5rem;
		font-size: 0.875rem;
		background-color: var(--color-surface);
		border: 1px solid var(--color-border);
		color: var(--color-text);
		outline: none;
	}
	.search-input:focus {
		border-color: var(--color-primary);
	}
	.search-clear {
		position: absolute;
		right: 0.5rem;
		top: 50%;
		transform: translateY(-50%);
		background: none;
		border: none;
		color: var(--color-text-muted);
		cursor: pointer;
		font-size: 1.25rem;
		line-height: 1;
	}

	.sort-toggle {
		display: flex;
		border-radius: 0.5rem;
		overflow: hidden;
		border: 1px solid var(--color-border);
	}
	.sort-btn {
		padding: 0.5rem 0.75rem;
		font-size: 0.75rem;
		background: var(--color-surface);
		color: var(--color-text-muted);
		border: none;
		cursor: pointer;
		transition: all 0.15s;
	}
	.sort-btn.active {
		background: var(--color-primary);
		color: var(--color-bg);
	}

	.category-chips {
		display: flex;
		gap: 0.375rem;
		flex-wrap: wrap;
		margin-bottom: 1rem;
		max-height: 4.5rem;
		overflow-y: auto;
	}
	.chip {
		display: inline-flex;
		align-items: center;
		gap: 0.375rem;
		padding: 0.25rem 0.625rem;
		border-radius: 999px;
		font-size: 0.6875rem;
		cursor: pointer;
		border: 1px solid var(--color-border);
		background: var(--color-surface);
		color: var(--color-text-muted);
		transition: all 0.15s;
		white-space: nowrap;
	}
	.chip.active {
		background: var(--color-primary);
		color: var(--color-bg);
		border-color: var(--color-primary);
	}
	.chip-count {
		font-size: 0.625rem;
		opacity: 0.7;
	}

	.error-banner {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0.75rem 1rem;
		border-radius: 0.5rem;
		margin-bottom: 1rem;
		background-color: rgba(239, 68, 68, 0.1);
		border: 1px solid var(--color-danger);
		color: var(--color-danger);
	}
	.error-dismiss {
		background: none;
		border: none;
		color: var(--color-danger);
		cursor: pointer;
		font-size: 1.25rem;
	}

	.card {
		display: flex;
		flex-direction: column;
		padding: 1rem;
		border-radius: 0.625rem;
		background-color: var(--color-surface);
		border: 1px solid var(--color-border);
		cursor: pointer;
		transition: border-color 0.15s, box-shadow 0.15s;
	}
	.card:hover {
		border-color: var(--color-primary-border);
		box-shadow: var(--shadow-glow);
	}

	.card-header {
		display: flex;
		align-items: flex-start;
		gap: 0.625rem;
		margin-bottom: 0.5rem;
	}
	.card-icon {
		width: 2rem;
		height: 2rem;
		border-radius: 0.375rem;
		object-fit: contain;
		flex-shrink: 0;
	}
	.card-icon-fallback {
		width: 2rem;
		height: 2rem;
		border-radius: 0.375rem;
		display: flex;
		align-items: center;
		justify-content: center;
		font-size: 0.875rem;
		font-weight: 600;
		flex-shrink: 0;
		background: var(--color-bg);
		color: var(--color-text-muted);
	}
	.card-title-group {
		flex: 1;
		min-width: 0;
	}
	.card-title {
		font-size: 0.875rem;
		font-weight: 600;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.card-badges {
		display: flex;
		gap: 0.25rem;
		margin-top: 0.125rem;
	}
	.badge {
		font-size: 0.5625rem;
		padding: 0.0625rem 0.375rem;
		border-radius: 999px;
		white-space: nowrap;
	}
	.badge-version {
		background: var(--color-bg);
		color: var(--color-text-muted);
	}
	.badge-stack {
		background: rgba(99, 102, 241, 0.15);
		color: var(--color-primary);
	}

	.card-desc {
		font-size: 0.75rem;
		color: var(--color-text-muted);
		line-height: 1.4;
		display: -webkit-box;
		-webkit-line-clamp: 2;
		-webkit-box-orient: vertical;
		overflow: hidden;
		flex: 1;
		margin-bottom: 0.5rem;
	}

	.card-meta {
		display: flex;
		align-items: center;
		justify-content: space-between;
		margin-bottom: 0.625rem;
	}
	.card-category {
		font-size: 0.625rem;
		padding: 0.125rem 0.5rem;
		border-radius: 999px;
		background: var(--color-bg);
		color: var(--color-text-muted);
	}
	.card-links {
		display: flex;
		gap: 0.5rem;
	}
	.card-links a {
		color: var(--color-text-muted);
		opacity: 0.6;
		transition: opacity 0.15s;
	}
	.card-links a:hover {
		opacity: 1;
		color: var(--color-primary);
	}

	.card-actions {
		display: flex;
		gap: 0.375rem;
	}
	.btn-configure {
		flex: 1;
		padding: 0.375rem 0.75rem;
		border-radius: 0.375rem;
		font-size: 0.75rem;
		cursor: pointer;
		background: var(--color-bg);
		color: var(--color-text);
		border: 1px solid var(--color-border);
		transition: border-color 0.15s;
	}
	.btn-configure:hover {
		border-color: var(--color-primary);
	}
	.btn-deploy {
		flex: 1;
		padding: 0.375rem 0.75rem;
		border-radius: 0.375rem;
		font-size: 0.75rem;
		font-weight: 500;
		cursor: pointer;
		background: var(--color-primary);
		color: var(--color-bg);
		border: none;
		transition: opacity 0.15s;
	}
	.btn-deploy:hover {
		opacity: 0.9;
	}
	.btn-deploy:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.import-bar {
		display: flex;
		gap: 0.5rem;
		align-items: flex-end;
		padding: 1rem;
		border-radius: 0.5rem;
		margin-bottom: 1rem;
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		flex-wrap: wrap;
	}
	.import-input {
		width: 100%;
		padding: 0.5rem 0.75rem;
		border-radius: 0.5rem;
		font-size: 0.875rem;
		background: var(--color-bg);
		border: 1px solid var(--color-border);
		color: var(--color-text);
		outline: none;
	}
	.import-btn {
		padding: 0.5rem 1rem;
		border-radius: 0.5rem;
		font-size: 0.875rem;
		font-weight: 500;
		cursor: pointer;
		background: var(--color-primary);
		color: var(--color-bg);
		border: none;
		white-space: nowrap;
	}

	/* Modal */
	.modal-backdrop {
		position: fixed;
		inset: 0;
		z-index: 50;
		display: flex;
		align-items: center;
		justify-content: center;
		padding: 1rem;
		background: rgba(0, 0, 0, 0.5);
		backdrop-filter: blur(4px);
	}
	.modal-content {
		width: 100%;
		max-width: 32rem;
		max-height: 90vh;
		overflow-y: auto;
		padding: 1.5rem;
		border-radius: 0.75rem;
		background: var(--color-surface);
		border: 1px solid var(--color-border);
	}
	.modal-header {
		display: flex;
		align-items: flex-start;
		gap: 0.75rem;
		margin-bottom: 1rem;
	}
	.modal-icon {
		width: 2.5rem;
		height: 2.5rem;
		border-radius: 0.5rem;
		object-fit: contain;
		flex-shrink: 0;
	}
	.modal-title {
		font-size: 1.125rem;
		font-weight: 700;
	}
	.modal-subtitle {
		font-size: 0.75rem;
		color: var(--color-text-muted);
		margin-top: 0.25rem;
		line-height: 1.4;
		display: -webkit-box;
		-webkit-line-clamp: 2;
		-webkit-box-orient: vertical;
		overflow: hidden;
	}
	.modal-links {
		display: flex;
		gap: 0.5rem;
		margin-bottom: 1rem;
		flex-wrap: wrap;
	}
	.modal-link {
		font-size: 0.6875rem;
		padding: 0.25rem 0.625rem;
		border-radius: 999px;
		background: var(--color-bg);
		color: var(--color-primary);
		text-decoration: none;
		text-transform: capitalize;
		border: 1px solid var(--color-border);
	}
	.modal-link:hover {
		border-color: var(--color-primary);
	}
	.modal-services {
		margin-bottom: 1rem;
	}
	.modal-services-label {
		font-size: 0.6875rem;
		color: var(--color-text-muted);
		margin-bottom: 0.375rem;
	}
	.modal-services-list {
		display: flex;
		gap: 0.25rem;
		flex-wrap: wrap;
	}
	.modal-service-chip {
		font-size: 0.625rem;
		padding: 0.125rem 0.5rem;
		border-radius: 999px;
		background: rgba(99, 102, 241, 0.1);
		color: var(--color-primary);
		font-family: monospace;
	}
	.modal-steps {
		display: flex;
		gap: 0.375rem;
		margin-bottom: 1.25rem;
	}
	.step-dot {
		width: 1.75rem;
		height: 1.75rem;
		border-radius: 50%;
		display: flex;
		align-items: center;
		justify-content: center;
		font-size: 0.75rem;
		font-weight: 500;
		background: var(--color-bg);
		color: var(--color-text-muted);
		transition: all 0.15s;
	}
	.step-dot.active {
		background: var(--color-primary);
		color: var(--color-bg);
	}
	.modal-actions {
		display: flex;
		gap: 0.5rem;
		justify-content: flex-end;
		margin-top: 1.5rem;
		align-items: center;
	}

	/* Summary */
	.summary {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
	}
	.summary-row {
		display: flex;
		justify-content: space-between;
		align-items: baseline;
		padding: 0.375rem 0;
		border-bottom: 1px solid var(--color-border);
	}
	.summary-label {
		font-size: 0.75rem;
		color: var(--color-text-muted);
		font-weight: 500;
	}
	.summary-value {
		font-size: 0.8125rem;
		text-align: right;
		max-width: 60%;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	/* Field components */
	.field-label {
		display: block;
		font-size: 0.8125rem;
		margin-bottom: 0.375rem;
		color: var(--color-text-muted);
	}
	.field-input {
		padding: 0.5rem 0.75rem;
		border-radius: 0.5rem;
		font-size: 0.875rem;
		background: var(--color-bg);
		border: 1px solid var(--color-border);
		color: var(--color-text);
		outline: none;
	}
	.field-input:focus {
		border-color: var(--color-primary);
	}
	.field-muted {
		color: var(--color-text-muted);
	}

	.btn-primary-sm {
		padding: 0.375rem 0.875rem;
		border-radius: 0.375rem;
		font-size: 0.8125rem;
		font-weight: 500;
		cursor: pointer;
		background: var(--color-primary);
		color: var(--color-bg);
		border: none;
	}
	.btn-primary-sm:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
	.btn-secondary-sm {
		padding: 0.375rem 0.75rem;
		border-radius: 0.375rem;
		font-size: 0.75rem;
		cursor: pointer;
		background: var(--color-bg);
		border: 1px solid var(--color-border);
		color: var(--color-text);
	}
	.btn-text-sm {
		padding: 0.375rem 0.75rem;
		border-radius: 0.375rem;
		font-size: 0.8125rem;
		cursor: pointer;
		background: none;
		border: none;
		color: var(--color-text-muted);
	}

	@media (max-width: 768px) {
		.category-chips {
			max-height: 3.5rem;
		}
		.search-box {
			max-width: 100%;
		}
		.modal-content {
			padding: 1rem;
		}
	}
</style>
