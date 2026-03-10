<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		variant = 'danger',
		class: className = '',
		children,
	}: {
		variant?: 'success' | 'danger' | 'warning' | 'info';
		class?: string;
		children: Snippet;
	} = $props();

	const styles: Record<string, { bg: string; border: string }> = {
		success: { bg: 'rgba(34, 197, 94, 0.08)', border: 'rgba(34, 197, 94, 0.25)' },
		danger: { bg: 'rgba(239, 68, 68, 0.06)', border: 'rgba(239, 68, 68, 0.2)' },
		warning: { bg: 'rgba(245, 158, 11, 0.08)', border: 'rgba(245, 158, 11, 0.25)' },
		info: { bg: 'rgba(59, 130, 246, 0.08)', border: 'rgba(59, 130, 246, 0.25)' },
	};
</script>

<div
	class="hive-alert {className}"
	style="background-color: {styles[variant].bg}; border-color: {styles[variant].border};"
>
	{@render children()}
</div>

<style>
	.hive-alert {
		border-radius: var(--radius-lg);
		padding: var(--space-md) var(--space-lg);
		border: 1px solid;
		animation: fade-scale-in var(--transition-slow);
	}
</style>
