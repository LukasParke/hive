<script lang="ts">
	import { api } from '$lib/api';
	import { goto } from '$app/navigation';
	import type { SearchResult } from '$lib/types';

	let { open = $bindable(false) }: { open: boolean } = $props();

	let query = $state('');
	let results = $state<SearchResult[]>([]);
	let selectedIndex = $state(0);
	let loading = $state(false);
	let inputEl = $state<HTMLInputElement | null>(null);
	let debounceTimer: ReturnType<typeof setTimeout>;

	const quickActions: SearchResult[] = [
		{ type: 'action', id: 'catalog', name: 'Deploy from Catalog', description: 'Browse app templates', url: '/catalog' },
		{ type: 'action', id: 'new-project', name: 'New Project', description: 'Create a project', url: '/projects' },
		{ type: 'action', id: 'logs', name: 'View Logs', description: 'System logs', url: '/logs' },
		{ type: 'action', id: 'nodes', name: 'Manage Nodes', description: 'View cluster nodes', url: '/nodes' },
	];

	$effect(() => {
		if (open) {
			query = '';
			results = [];
			selectedIndex = 0;
			setTimeout(() => inputEl?.focus(), 50);
		}
	});

	function handleInput() {
		clearTimeout(debounceTimer);
		if (!query.trim()) {
			results = [];
			return;
		}
		debounceTimer = setTimeout(async () => {
			loading = true;
			try {
				results = await api.search(query);
			} catch {
				results = [];
			}
			loading = false;
			selectedIndex = 0;
		}, 300);
	}

	function handleKeydown(e: KeyboardEvent) {
		const items = displayItems;
		if (e.key === 'ArrowDown') {
			e.preventDefault();
			selectedIndex = Math.min(selectedIndex + 1, items.length - 1);
		} else if (e.key === 'ArrowUp') {
			e.preventDefault();
			selectedIndex = Math.max(selectedIndex - 1, 0);
		} else if (e.key === 'Enter') {
			e.preventDefault();
			const item = items[selectedIndex];
			if (item) selectItem(item);
		} else if (e.key === 'Escape') {
			open = false;
		}
	}

	function selectItem(item: SearchResult) {
		open = false;
		goto(item.url);
	}

	const displayItems = $derived(query.trim() ? results : quickActions);

	const typeIcons: Record<string, string> = {
		project: '📁', app: '📦', node: '🖥️', template: '📋',
		action: '⚡', secret: '🔐', volume: '💾', stack: '🗂️',
	};
</script>

{#if open}
<div class="fixed inset-0 z-50 flex items-start justify-center pt-[15vh]"
	role="dialog" aria-modal="true">
	<button class="absolute inset-0 bg-black/50 backdrop-blur-sm" onclick={() => open = false} tabindex="-1" aria-label="Close"></button>
	<div class="relative w-full max-w-lg command-palette-shell overflow-hidden">
		<div class="flex items-center gap-2 px-4 py-3 border-b" style="border-color: var(--color-border);">
			<svg class="w-5 h-5 shrink-0" style="color: var(--color-text-muted);" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"/>
			</svg>
			<input
				bind:this={inputEl}
				bind:value={query}
				oninput={handleInput}
				onkeydown={handleKeydown}
				placeholder="Search projects, apps, nodes..."
				class="flex-1 bg-transparent text-sm outline-none"
				style="color: var(--color-text);"
			/>
			<kbd class="kbd-chip">ESC</kbd>
		</div>

		<div class="max-h-80 overflow-y-auto">
			{#if loading}
				<div class="p-4 text-center text-sm text-muted">Searching...</div>
			{:else if displayItems.length === 0 && query}
				<div class="p-4 text-center text-sm text-muted">No results found</div>
			{:else}
				{#each displayItems as item, i}
					<button
						class="w-full flex items-center gap-3 px-4 py-2.5 text-left transition-colors"
						style="background-color: {i === selectedIndex ? 'var(--color-surface-active)' : 'transparent'};"
						onclick={() => selectItem(item)}
					>
						<span class="text-lg">{typeIcons[item.type] || '📄'}</span>
						<div class="flex-1 min-w-0">
							<div class="text-sm truncate" style="color: var(--color-text);">{item.name}</div>
							{#if item.description}
								<div class="text-xs truncate text-muted">{item.description}</div>
							{/if}
						</div>
						<span class="kbd-chip capitalize">{item.type}</span>
					</button>
				{/each}
			{/if}
		</div>

		{#if !query}
			<div class="px-4 py-2 border-t text-[10px] text-muted" style="border-color: var(--color-border);">
				Type to search or select a quick action
			</div>
		{/if}
	</div>
</div>
{/if}
