<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		class: className = '',
		children,
	}: {
		class?: string;
		children: Snippet;
	} = $props();
</script>

<div class="stat-card {className}">
	{@render children()}
</div>

<style>
	.stat-card {
		border-radius: var(--radius-lg);
		padding: var(--space-lg);
		background-color: var(--color-surface);
		border: 1px solid var(--color-border);
		transition: border-color var(--transition-base), box-shadow var(--transition-base);
	}
	.stat-card:hover {
		border-color: var(--color-primary-border);
		box-shadow: var(--shadow-glow);
	}
	@media (max-width: 768px) {
		.stat-card {
			padding: var(--space-md);
		}
	}
</style>
