<script lang="ts">
	import { api, type RegistryStatus, type RegistryImage } from '$lib/api';
	import { onMount } from 'svelte';

	let status = $state<RegistryStatus | null>(null);
	let images = $state<RegistryImage[]>([]);
	let error = $state('');

	onMount(async () => {
		await loadData();
	});

	async function loadData() {
		try {
			[status, images] = await Promise.all([
				api.registryStatus(),
				api.registryImages().catch(() => []),
			]);
		} catch (e: any) {
			error = e.message;
		}
	}

	async function deleteImage(name: string, tag: string) {
		try {
			await api.registryDeleteImage(name, tag);
			await loadData();
		} catch (e: any) {
			error = e.message;
		}
	}
</script>

<div class="max-w-4xl mx-auto">
	<h2 class="text-2xl font-bold mb-6">Container Registry</h2>

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid #ef4444;">
			<p style="color: #ef4444;">{error}</p>
		</div>
	{/if}

	{#if status}
		<div class="grid grid-cols-1 md:grid-cols-3 gap-4 mb-6">
			<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">Status</p>
				<p class="text-lg font-semibold" style="color: {status.running ? '#22c55e' : '#ef4444'};">
					{status.running ? 'Running' : 'Not Running'}
				</p>
			</div>
			<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">Images</p>
				<p class="text-lg font-semibold">{status.image_count ?? 0}</p>
			</div>
			<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">Endpoint</p>
				<p class="text-sm font-mono">127.0.0.1:5000</p>
			</div>
		</div>
	{/if}

	<h3 class="text-lg font-semibold mb-4">Images</h3>
	<div class="space-y-3">
		{#each images as image}
			<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<p class="font-medium mb-2">{image.name}</p>
				<div class="flex flex-wrap gap-2">
					{#each image.tags ?? [] as tag}
						<div class="flex items-center gap-1 px-2 py-1 rounded text-xs" style="background-color: var(--color-bg); border: 1px solid var(--color-border);">
							<span class="font-mono">{tag}</span>
							<button onclick={() => deleteImage(image.name, tag)} class="ml-1" style="color: #ef4444;">x</button>
						</div>
					{/each}
				</div>
			</div>
		{:else}
			<p class="text-sm" style="color: var(--color-text-muted);">No images in registry</p>
		{/each}
	</div>
</div>
