<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		variant?: 'success' | 'warning' | 'danger' | 'info' | 'neutral' | 'primary';
		dot?: boolean;
		class?: string;
		children: Snippet;
	}

	let {
		variant = 'neutral',
		dot = false,
		class: className = '',
		children,
	}: Props = $props();

	const dotColor: Record<string, string> = {
		success: 'var(--color-success)',
		warning: 'var(--color-warning)',
		danger: 'var(--color-danger)',
		info: 'var(--color-info)',
		neutral: 'var(--color-text-muted)',
		primary: 'var(--color-primary)',
	};
</script>

<span class="badge badge-{variant} {className}">
	{#if dot}
		<span class="badge-dot" style:background-color={dotColor[variant]}></span>
	{/if}
	{@render children()}
</span>
