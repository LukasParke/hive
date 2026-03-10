<script lang="ts">
	import { api } from '$lib/api';
	import type { GitSource, GitRepository, GitBranch } from '$lib/types';
	import { invalidateAll } from '$app/navigation';
	import { page } from '$app/stores';

	let { data } = $props();

	let error = $state('');
	let success = $state('');
	let ghLoading = $state(false);

	// PAT form state
	let showPATForm = $state(false);
	let patProvider = $state('gitlab');
	let patToken = $state('');
	let patName = $state('');

	// Browse repos modal
	let browseSource = $state<GitSource | null>(null);
	let repos = $state<GitRepository[]>([]);
	let searchQuery = $state('');
	let loadingRepos = $state(false);

	let linkRepo = $state<GitRepository | null>(null);
	let branches = $state<GitBranch[]>([]);
	let selectedBranch = $state('');
	let linking = $state(false);

	// Handle GitHub redirect params
	$effect(() => {
		const url = new URL($page.url);
		const code = url.searchParams.get('code');
		const setup = url.searchParams.get('setup');
		const installationId = url.searchParams.get('installation_id');

		if (code && setup === 'complete') {
			completeGitHubApp(code);
			window.history.replaceState({}, '', '/git');
		} else if (installationId && setup === 'install') {
			saveInstallation(parseInt(installationId));
			window.history.replaceState({}, '', '/git');
		}
	});

	async function connectGitHub() {
		ghLoading = true;
		error = '';
		try {
			const result = await api.githubAppManifest();
			const form = document.createElement('form');
			form.method = 'POST';
			form.action = result.redirect_url;
			const input = document.createElement('input');
			input.type = 'hidden';
			input.name = 'manifest';
			input.value = result.manifest;
			form.appendChild(input);
			document.body.appendChild(form);
			form.submit();
		} catch (e: any) {
			error = e.message;
			ghLoading = false;
		}
	}

	async function completeGitHubApp(code: string) {
		ghLoading = true;
		error = '';
		try {
			const result = await api.githubAppComplete(code);
			success = `GitHub App "${result.slug}" created successfully! Now install it on your account.`;
			await invalidateAll();
		} catch (e: any) {
			error = 'Failed to complete GitHub App setup: ' + e.message;
		}
		ghLoading = false;
	}

	async function saveInstallation(installationId: number) {
		ghLoading = true;
		error = '';
		try {
			await api.githubAppInstallation(installationId);
			success = 'GitHub App installed successfully! Push-to-deploy is now active.';
			await invalidateAll();
		} catch (e: any) {
			error = 'Failed to save installation: ' + e.message;
		}
		ghLoading = false;
	}

	async function disconnectGitHub() {
		if (!confirm('Disconnect the GitHub App? This will disable push-to-deploy for GitHub repos.')) return;
		ghLoading = true;
		error = '';
		try {
			await api.githubAppDelete();
			success = 'GitHub App disconnected.';
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		}
		ghLoading = false;
	}

	async function addPATSource() {
		if (!patToken.trim()) {
			error = 'Token is required';
			return;
		}
		try {
			await api.createGitSource({ provider: patProvider, provider_name: patName || undefined, token: patToken });
			showPATForm = false;
			patToken = '';
			patName = '';
			success = 'Git source connected.';
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		}
	}

	async function removeSource(id: string, provider: string) {
		if (!confirm(`Remove ${provider} connection?`)) return;
		try {
			await api.deleteGitSource(id);
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		}
	}

	async function openBrowse(source: GitSource) {
		browseSource = source;
		repos = [];
		searchQuery = '';
		loadingRepos = true;
		try {
			repos = await api.listGitRepos(source.id);
		} catch (e: any) {
			error = e.message;
			browseSource = null;
		}
		loadingRepos = false;
	}

	function closeBrowse() {
		browseSource = null;
		linkRepo = null;
		branches = [];
	}

	const filteredRepos = $derived(
		searchQuery.trim()
			? repos.filter((r) => r.full_name.toLowerCase().includes(searchQuery.toLowerCase()) || r.name.toLowerCase().includes(searchQuery.toLowerCase()))
			: repos
	);

	async function startLink(repo: GitRepository) {
		linkRepo = repo;
		branches = [];
		selectedBranch = repo.default_branch || 'main';
		try {
			branches = await api.listGitRepoBranches(browseSource!.id, repo.full_name);
			if (branches.length > 0 && !branches.find((b) => b.name === selectedBranch)) {
				const def = branches.find((b) => b.is_default);
				selectedBranch = def ? def.name : branches[0].name;
			}
		} catch (e: any) {
			error = e.message;
			linkRepo = null;
		}
	}

	function cancelLink() { linkRepo = null; }

	async function confirmLink() {
		if (!linkRepo || !browseSource) return;
		linking = true;
		try {
			await api.registerWebhook(browseSource.id, linkRepo.full_name);
			linkRepo = null;
			closeBrowse();
		} catch (e: any) {
			error = e.message;
		}
		linking = false;
	}
</script>

<svelte:head><title>Git Integration | Hive</title></svelte:head>

<div class="max-w-4xl mx-auto p-6">
	<h2 class="text-2xl font-bold mb-2">Git Integration</h2>
	<p class="text-sm mb-8" style="color: var(--color-text-muted);">Connect your Git providers for repo access, webhooks, and push-to-deploy.</p>

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);">
			<p class="text-sm" style="color: var(--color-danger);">{error}</p>
			<button onclick={() => (error = '')} class="text-xs mt-1 underline" style="color: var(--color-danger);">Dismiss</button>
		</div>
	{/if}

	{#if success}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(34, 197, 94, 0.1); border: 1px solid var(--color-success);">
			<p class="text-sm" style="color: var(--color-success);">{success}</p>
			<button onclick={() => (success = '')} class="text-xs mt-1 underline" style="color: var(--color-success);">Dismiss</button>
		</div>
	{/if}

	<!-- GitHub App section -->
	<div class="section-card mb-6">
		<div class="flex items-center gap-3 mb-4">
			<svg class="w-8 h-8" viewBox="0 0 24 24" fill="currentColor">
				<path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z"/>
			</svg>
			<div>
				<h3 class="text-lg font-semibold">GitHub</h3>
				<p class="text-xs" style="color: var(--color-text-muted);">One-click GitHub App for repo access, webhooks, and push-to-deploy</p>
			</div>
		</div>

		{#if data.ghStatus.configured}
			<div class="rounded-lg p-4 mb-3" style="background-color: var(--color-bg); border: 1px solid var(--color-border);">
				<div class="flex items-center justify-between">
					<div class="flex items-center gap-3">
						<span class="status-dot" class:connected={data.ghStatus.installed} class:pending={!data.ghStatus.installed}></span>
						<div>
							<p class="text-sm font-medium">{data.ghStatus.slug || 'GitHub App'}</p>
							<p class="text-xs" style="color: var(--color-text-muted);">
								{#if data.ghStatus.installed}
									Installed &mdash; push-to-deploy active
								{:else}
									App created, awaiting installation
								{/if}
							</p>
						</div>
					</div>
					<div class="flex gap-2">
						{#if !data.ghStatus.installed && data.ghStatus.html_url}
							<a
								href="{data.ghStatus.html_url}/installations/new"
								target="_blank"
								class="px-3 py-1.5 rounded text-xs font-medium text-white"
								style="background-color: var(--color-primary);"
							>
								Install App
							</a>
						{/if}
						<button
							onclick={disconnectGitHub}
							disabled={ghLoading}
							class="px-3 py-1.5 rounded text-xs font-medium"
							style="border: 1px solid rgba(239,68,68,0.3); color: var(--color-danger);"
						>
							{ghLoading ? '...' : 'Disconnect'}
						</button>
					</div>
				</div>
			</div>
		{:else}
			<button
				onclick={connectGitHub}
				disabled={ghLoading}
				class="connect-btn github"
			>
				{#if ghLoading}
					<span class="loading-spinner"></span>
					Connecting...
				{:else}
					<svg class="w-5 h-5" viewBox="0 0 24 24" fill="currentColor">
						<path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z"/>
					</svg>
					Connect GitHub
				{/if}
			</button>
			<p class="text-xs mt-2" style="color: var(--color-text-muted);">
				Creates a GitHub App on your account with repo read access and webhook permissions. No token management needed.
			</p>
		{/if}
	</div>

	<!-- Other Git sources (PAT-based) -->
	<div class="section-card">
		<div class="flex items-center justify-between mb-4">
			<div>
				<h3 class="text-lg font-semibold">Other Git Sources</h3>
				<p class="text-xs" style="color: var(--color-text-muted);">Connect GitLab, Gitea, or other providers with a Personal Access Token</p>
			</div>
			<button
				onclick={() => { showPATForm = !showPATForm; if (!showPATForm) { patToken = ''; patName = ''; } }}
				class="px-3 py-1.5 rounded text-xs font-medium"
				style="border: 1px solid var(--color-primary); color: var(--color-primary);"
			>
				{showPATForm ? 'Cancel' : 'Add Source'}
			</button>
		</div>

		{#if showPATForm}
			<div class="rounded-lg p-4 mb-4" style="background-color: var(--color-bg); border: 1px solid var(--color-border);">
				<div class="mb-3">
					<span class="block text-xs font-medium mb-2">Provider</span>
					<div class="flex gap-2">
						{#each [
							{ id: 'gitlab', label: 'GitLab' },
							{ id: 'gitea', label: 'Gitea' },
							{ id: 'github', label: 'GitHub (PAT)' }
						] as p}
							<button
								onclick={() => (patProvider = p.id)}
								class="px-3 py-1.5 rounded text-xs"
								style="border: 1px solid {patProvider === p.id ? 'var(--color-primary)' : 'var(--color-border)'}; background-color: {patProvider === p.id ? 'var(--color-primary)' : 'transparent'}; color: {patProvider === p.id ? 'white' : 'inherit'};"
							>
								{p.label}
							</button>
						{/each}
					</div>
				</div>
				{#if patProvider === 'gitea'}
					<div class="mb-3">
						<label for="pat-name" class="block text-xs mb-1" style="color: var(--color-text-muted);">Server URL</label>
						<input
							id="pat-name"
							type="text"
							placeholder="https://gitea.example.com"
							bind:value={patName}
							class="w-full rounded px-3 py-2 text-sm"
							style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
						/>
					</div>
				{/if}
				<div class="mb-3">
					<label for="pat-token" class="block text-xs mb-1" style="color: var(--color-text-muted);">Personal Access Token</label>
					<input
						id="pat-token"
						type="password"
						placeholder={patProvider === 'github' ? 'ghp_xxx' : patProvider === 'gitlab' ? 'glpat-xxx' : 'token'}
						bind:value={patToken}
						class="w-full rounded px-3 py-2 text-sm"
						style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
					/>
					<p class="text-xs mt-1" style="color: var(--color-text-muted);">
						{#if patProvider === 'github'}
							Create at <a href="https://github.com/settings/tokens" target="_blank" class="underline">github.com/settings/tokens</a> with repo scope
						{:else if patProvider === 'gitlab'}
							Create at <a href="https://gitlab.com/-/user_settings/personal_access_tokens" target="_blank" class="underline">GitLab Settings</a> with read_api scope
						{:else}
							Create in your Gitea instance under Settings &rarr; Applications
						{/if}
					</p>
				</div>
				<button onclick={addPATSource} class="px-4 py-2 rounded text-sm font-medium text-white" style="background-color: var(--color-primary);">
					Connect
				</button>
			</div>
		{/if}

		<div class="space-y-2">
			{#each data.sources ?? [] as src}
				<div class="rounded-lg p-4 flex items-center justify-between" style="background-color: var(--color-bg); border: 1px solid var(--color-border);">
					<div class="flex items-center gap-3">
						<span class="text-lg">
							{#if src.provider === 'github'}&#128025;{:else if src.provider === 'gitlab'}&#129418;{:else}&#128230;{/if}
						</span>
						<div>
							<p class="text-sm font-medium capitalize">{src.provider}{src.provider_name ? ` (${src.provider_name})` : ''}</p>
							<p class="text-xs" style="color: var(--color-text-muted);">Added {new Date(src.created_at).toLocaleDateString()}</p>
						</div>
					</div>
					<div class="flex gap-2">
						<button
							onclick={() => openBrowse(src)}
							class="px-3 py-1.5 rounded text-xs font-medium"
							style="border: 1px solid var(--color-primary); color: var(--color-primary);"
						>
							Browse Repos
						</button>
						<button
							onclick={() => removeSource(src.id, src.provider)}
							class="px-3 py-1.5 rounded text-xs font-medium"
							style="border: 1px solid rgba(239,68,68,0.3); color: var(--color-danger);"
						>
							Remove
						</button>
					</div>
				</div>
			{/each}
			{#if (data.sources ?? []).length === 0 && !showPATForm}
				<p class="text-sm py-4 text-center" style="color: var(--color-text-muted);">No PAT sources configured yet.</p>
			{/if}
		</div>
	</div>
</div>

<!-- Repo browse modal -->
{#if browseSource}
	<div
		class="fixed inset-0 z-50 flex items-center justify-center"
		style="background-color: rgba(0,0,0,0.5);"
		onclick={(e) => e.target === e.currentTarget && !linkRepo && closeBrowse()}
		onkeydown={(e) => e.key === 'Escape' && !linkRepo && closeBrowse()}
		role="dialog"
		tabindex="-1"
	>
		<div
			class="rounded-lg max-w-2xl w-full max-h-[80vh] overflow-hidden flex flex-col"
			style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
			onclick={(e) => e.stopPropagation()}
		>
			<div class="p-4 border-b flex items-center justify-between" style="border-color: var(--color-border);">
				<h3 class="text-lg font-semibold">Browse repositories</h3>
				<button onclick={() => !linkRepo && closeBrowse()} class="text-lg">&times;</button>
			</div>
			{#if linkRepo}
				<div class="p-4 space-y-4">
					<p class="text-sm">Link <strong>{linkRepo.full_name}</strong></p>
					<div>
						<label class="block text-sm mb-1" style="color: var(--color-text-muted);">Branch</label>
						<select
							bind:value={selectedBranch}
							class="w-full rounded px-3 py-2 text-sm"
							style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
						>
							{#each branches as b}
								<option value={b.name}>{b.name}{b.is_default ? ' (default)' : ''}</option>
							{/each}
						</select>
					</div>
					<div class="flex gap-2">
						<button
							onclick={confirmLink}
							disabled={linking}
							class="px-4 py-2 rounded text-sm font-medium text-white"
							style="background-color: var(--color-primary);"
						>
							{linking ? 'Linking...' : 'Link'}
						</button>
						<button
							onclick={cancelLink}
							class="px-4 py-2 rounded text-sm"
							style="border: 1px solid var(--color-border);"
						>
							Cancel
						</button>
					</div>
				</div>
			{:else}
				<div class="p-4 border-b" style="border-color: var(--color-border);">
					<input
						type="text"
						placeholder="Search repos..."
						bind:value={searchQuery}
						class="w-full rounded px-3 py-2 text-sm"
						style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
					/>
				</div>
				<div class="flex-1 overflow-y-auto p-4">
					{#if loadingRepos}
						<p class="text-sm" style="color: var(--color-text-muted);">Loading...</p>
					{:else}
						<div class="space-y-2">
							{#each filteredRepos as repo}
								<div
									class="flex items-center justify-between py-2 px-3 rounded"
									style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
								>
									<div>
										<p class="text-sm font-medium">{repo.full_name}</p>
										{#if repo.description}
											<p class="text-xs truncate max-w-md" style="color: var(--color-text-muted);">{repo.description}</p>
										{/if}
									</div>
									<button
										onclick={() => startLink(repo)}
										class="px-3 py-1 rounded text-xs font-medium"
										style="background-color: var(--color-primary); color: white;"
									>
										Link
									</button>
								</div>
							{/each}
							{#if filteredRepos.length === 0}
								<p class="text-sm py-4" style="color: var(--color-text-muted);">No repositories found</p>
							{/if}
						</div>
					{/if}
				</div>
			{/if}
		</div>
	</div>
{/if}

<style>
	.section-card {
		padding: 1.5rem;
		border-radius: var(--radius-lg);
		background-color: var(--color-surface);
		border: 1px solid var(--color-border);
	}

	.connect-btn {
		display: inline-flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.625rem 1.25rem;
		border-radius: var(--radius-md);
		font-size: 0.875rem;
		font-weight: 600;
		cursor: pointer;
		transition: opacity 0.15s;
		border: none;
	}
	.connect-btn:hover:not(:disabled) { opacity: 0.9; }
	.connect-btn:disabled { opacity: 0.6; cursor: not-allowed; }
	.connect-btn.github {
		background-color: #24292e;
		color: white;
	}

	.status-dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		flex-shrink: 0;
	}
	.status-dot.connected { background-color: var(--color-success); }
	.status-dot.pending { background-color: var(--color-warning); }

	.loading-spinner {
		width: 16px;
		height: 16px;
		border: 2px solid rgba(255,255,255,0.3);
		border-top-color: white;
		border-radius: 50%;
		animation: spin 0.6s linear infinite;
	}
	@keyframes spin { to { transform: rotate(360deg); } }
</style>
