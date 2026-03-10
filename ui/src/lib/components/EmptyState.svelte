<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		title?: string;
		description?: string;
		icon?: Snippet;
		action?: Snippet;
	}

	let {
		title = 'Nothing here yet',
		description,
		icon,
		action,
	}: Props = $props();
</script>

<div class="empty-state">
	{#if icon}
		{@render icon()}
	{:else}
		<svg class="empty-state-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
			<circle cx="12" cy="12" r="10" />
			<path d="M12 8v4M12 16h.01" />
		</svg>
	{/if}
	<div>
		<p style="font-weight: 500; color: var(--color-text-secondary)">{title}</p>
		{#if description}
			<p style="font-size: var(--text-sm); margin-top: var(--space-xs)">{description}</p>
		{/if}
	</div>
	{#if action}
		<div style="margin-top: var(--space-sm)">
			{@render action()}
		</div>
	{/if}
</div>
