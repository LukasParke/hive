<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		title = '',
		class: className = '',
		children,
		headerRight,
	}: {
		title?: string;
		class?: string;
		children: Snippet;
		headerRight?: Snippet;
	} = $props();
</script>

<section class="hive-panel {className}">
	{#if title || headerRight}
		<div class="hive-panel-header">
			{#if title}
				<h3 class="hive-panel-title">{title}</h3>
			{/if}
			{#if headerRight}
				<div class="hive-panel-header-right">
					{@render headerRight()}
				</div>
			{/if}
		</div>
	{/if}
	<div class="hive-panel-body">
		{@render children()}
	</div>
</section>

<style>
	.hive-panel {
		border-radius: var(--radius-lg);
		background-color: var(--color-surface);
		border: 1px solid var(--color-border);
		overflow: hidden;
	}
	.hive-panel-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0.75rem 1.25rem;
		border-bottom: 1px solid var(--color-border);
		gap: var(--space-md);
	}
	.hive-panel-title {
		font-size: var(--text-base);
		font-weight: 600;
	}
	.hive-panel-header-right {
		display: flex;
		align-items: center;
		gap: var(--space-sm);
	}
	.hive-panel-body {
		padding: 1.25rem;
	}
	@media (max-width: 768px) {
		.hive-panel-body {
			padding: 1rem;
		}
		.hive-panel-header {
			padding: 0.625rem 1rem;
		}
	}
</style>
