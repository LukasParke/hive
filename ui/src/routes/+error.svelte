<script lang="ts">
	import HiveLogo from '$lib/components/HiveLogo.svelte';
	import { page } from '$app/state';
</script>

<div class="min-h-screen flex items-center justify-center p-4" style="background-color: var(--color-bg);">
	<div class="w-full max-w-md text-center">
		<div class="flex justify-center mb-4">
			<HiveLogo size={48} />
		</div>

		<div class="hex-badge">
			<span class="text-4xl font-bold" style="color: var(--color-primary);">
				{page.status}
			</span>
		</div>

		<h1 class="text-xl font-semibold mb-2 mt-4" style="color: var(--color-text);">
			{#if page.status === 404}
				Page not found
			{:else if page.status === 403}
				Access denied
			{:else if page.status === 401}
				Not authenticated
			{:else}
				Something went wrong
			{/if}
		</h1>

		<p class="text-sm mb-6" style="color: var(--color-text-muted);">
			{page.error?.message || 'An unexpected error occurred.'}
		</p>

		{#if page.error?.errorId}
			<p class="text-xs mb-6 font-mono" style="color: var(--color-text-muted);">
				Reference: {page.error.errorId}
			</p>
		{/if}

		<div class="flex gap-3 justify-center">
			<a
				href="/"
				class="px-4 py-2 rounded-lg text-sm font-medium transition-colors"
				style="background-color: var(--color-primary); color: var(--color-bg);"
			>
				Go home
			</a>
			<button
				onclick={() => history.back()}
				class="px-4 py-2 rounded-lg text-sm font-medium transition-colors cursor-pointer"
				style="background-color: var(--color-surface); color: var(--color-text); border: 1px solid var(--color-border);"
			>
				Go back
			</button>
		</div>
	</div>
</div>

<style>
	.hex-badge {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 96px;
		height: 96px;
		clip-path: polygon(50% 0%, 100% 25%, 100% 75%, 50% 100%, 0% 75%, 0% 25%);
		background-color: var(--color-surface);
		box-shadow: 0 0 40px rgba(229, 160, 13, 0.1);
	}
</style>
