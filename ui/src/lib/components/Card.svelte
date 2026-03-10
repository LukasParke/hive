<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		interactive?: boolean;
		class?: string;
		children: Snippet;
		header?: Snippet;
		footer?: Snippet;
	}

	let {
		interactive = false,
		class: className = '',
		children,
		header,
		footer,
	}: Props = $props();
</script>

<div class="hive-card {interactive ? 'hive-card-interactive' : ''} {className}">
	{#if header}
		<div class="hive-card-header">
			{@render header()}
		</div>
	{/if}
	<div class="hive-card-body">
		{@render children()}
	</div>
	{#if footer}
		<div class="hive-card-footer">
			{@render footer()}
		</div>
	{/if}
</div>
