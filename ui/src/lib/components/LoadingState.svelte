<script lang="ts">
	interface Props {
		message?: string;
		lines?: number;
	}

	let {
		message,
		lines = 0,
	}: Props = $props();
</script>

{#if lines > 0}
	<div class="flex flex-col gap-3" style="padding: var(--space-lg)">
		{#each Array(lines) as _, i}
			<div class="skeleton" style:height="1rem" style:width="{80 - i * 10}%"></div>
		{/each}
	</div>
{:else}
	<div class="flex items-center justify-center gap-3" style="padding: var(--space-2xl)">
		<span class="spinner"></span>
		{#if message}
			<span class="text-muted" style="font-size: var(--text-sm)">{message}</span>
		{/if}
	</div>
{/if}
