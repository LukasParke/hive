<script lang="ts">
	import { api, type GitSource, type GitRepository, type GitBranch } from '$lib/api';
	import { onMount } from 'svelte';

	let sources = $state<GitSource[]>([]);
	let error = $state('');
	let showForm = $state(false);
	let newProvider = $state('github');
	let newToken = $state('');

	let browseSource = $state<GitSource | null>(null);
	let repos = $state<GitRepository[]>([]);
	let searchQuery = $state('');
	let loadingRepos = $state(false);

	let linkRepo = $state<GitRepository | null>(null);
	let branches = $state<GitBranch[]>([]);
	let selectedBranch = $state('');
	let registerWebhook = $state(true);
	let linking = $state(false);

	onMount(load);

	async function load() {
		try {
			sources = await api.listGitSources();
		} catch (e: any) {
			error = e.message;
		}
	}

	async function addSource() {
		if (!newToken.trim()) {
			error = 'Token is required';
			return;
		}
		try {
			await api.createGitSource({ provider: newProvider, token: newToken });
			showForm = false;
			newToken = '';
			await load();
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
		registerWebhook = true;
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

	function cancelLink() {
		linkRepo = null;
	}

	async function confirmLink() {
		if (!linkRepo || !browseSource) return;
		linking = true;
		try {
			if (registerWebhook) {
				await api.registerWebhook(browseSource.id, linkRepo.full_name);
			}
			linkRepo = null;
			closeBrowse();
		} catch (e: any) {
			error = e.message;
		}
		linking = false;
	}

	function providerIcon(p: string): string {
		const n = (p || '').toLowerCase();
		if (n.includes('github')) return '🐙';
		if (n.includes('gitlab')) return '🦊';
		return '📦';
	}
</script>

<div class="max-w-4xl mx-auto p-6">
	<div class="flex items-center justify-between mb-6">
		<h2 class="text-2xl font-bold">Git Providers</h2>
		<button
			onclick={() => {
				showForm = !showForm;
				if (!showForm) newToken = '';
			}}
			class="px-4 py-2 rounded text-sm font-medium text-white"
			style="background-color: var(--color-primary);"
		>
			{showForm ? 'Cancel' : 'Add Source'}
		</button>
	</div>

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid #ef4444;">
			<p style="color: #ef4444;">{error}</p>
			<button onclick={() => (error = '')} class="text-xs mt-1 underline" style="color: #ef4444;">Dismiss</button>
		</div>
	{/if}

	{#if showForm}
		<div class="rounded-lg p-5 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<div class="mb-4">
				<span class="block text-sm font-medium mb-2">Provider</span>
				<div class="flex gap-2">
					<button
						onclick={() => (newProvider = 'github')}
						class="px-3 py-1.5 rounded text-sm"
						style="border: 1px solid {newProvider === 'github' ? 'var(--color-primary)' : 'var(--color-border)'}; background-color: {newProvider === 'github' ? 'var(--color-primary)' : 'transparent'}; color: {newProvider === 'github' ? 'white' : 'inherit'};"
					>
						GitHub
					</button>
					<button
						onclick={() => (newProvider = 'gitlab')}
						class="px-3 py-1.5 rounded text-sm"
						style="border: 1px solid {newProvider === 'gitlab' ? 'var(--color-primary)' : 'var(--color-border)'}; background-color: {newProvider === 'gitlab' ? 'var(--color-primary)' : 'transparent'}; color: {newProvider === 'gitlab' ? 'white' : 'inherit'};"
					>
						GitLab
					</button>
				</div>
			</div>
			<div class="mb-4">
				<label for="token" class="block text-sm mb-1" style="color: var(--color-text-muted);">Personal Access Token</label>
				<input
					id="token"
					type="password"
					placeholder="ghp_xxx or glpat-xxx"
					bind:value={newToken}
					class="w-full rounded px-3 py-2 text-sm"
					style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
				/>
				<p class="text-xs mt-1" style="color: var(--color-text-muted);">
					Create a token with repo scope at {newProvider === 'github' ? 'github.com/settings/tokens' : 'gitlab.com/-/user_settings/personal_access_tokens'}
				</p>
			</div>
			<button onclick={addSource} class="px-4 py-2 rounded text-sm font-medium text-white" style="background-color: var(--color-primary);">
				Connect
			</button>
		</div>
	{/if}

	<div class="space-y-3">
		{#each sources as src}
			<div class="rounded-lg p-4 flex items-center justify-between" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<div class="flex items-center gap-3">
					<span class="text-xl">{providerIcon(src.provider)}</span>
					<div>
						<p class="text-sm font-medium capitalize">{src.provider}</p>
						<p class="text-xs" style="color: var(--color-text-muted);">Added {new Date(src.created_at).toLocaleDateString()}</p>
					</div>
				</div>
				<button
					onclick={() => openBrowse(src)}
					class="px-3 py-1.5 rounded text-xs font-medium"
					style="border: 1px solid var(--color-primary); color: var(--color-primary);"
				>
					Browse Repos
				</button>
			</div>
		{/each}
		{#if sources.length === 0 && !showForm}
			<div class="text-center py-12 rounded-lg" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<p class="text-lg mb-2" style="color: var(--color-text-muted);">No git sources configured</p>
				<p class="text-sm mb-4" style="color: var(--color-text-muted);">Connect GitHub or GitLab to browse repos and enable push-to-deploy</p>
				<button
					onclick={() => (showForm = true)}
					class="px-4 py-2 rounded text-sm font-medium text-white"
					style="background-color: var(--color-primary);"
				>
					Add Git Source
				</button>
			</div>
		{/if}
	</div>
</div>

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
					<label class="flex items-center gap-2 cursor-pointer">
						<input type="checkbox" bind:checked={registerWebhook} />
						<span class="text-sm">Register webhook for push-to-deploy</span>
					</label>
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
