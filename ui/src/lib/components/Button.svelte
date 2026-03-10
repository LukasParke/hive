<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLButtonAttributes } from 'svelte/elements';

	interface Props extends HTMLButtonAttributes {
		variant?: 'primary' | 'secondary' | 'ghost' | 'danger' | 'danger-filled';
		size?: 'sm' | 'md' | 'lg';
		loading?: boolean;
		icon?: boolean;
		children: Snippet;
	}

	let {
		variant = 'secondary',
		size = 'md',
		loading = false,
		icon = false,
		children,
		class: className = '',
		disabled,
		...rest
	}: Props = $props();

	const sizeClass = $derived(
		size === 'sm' ? 'btn-sm' : size === 'lg' ? 'btn-lg' : ''
	);

	const variantClass = $derived(
		variant === 'danger-filled' ? 'btn-danger-filled' : `btn-${variant}`
	);
</script>

<button
	class="btn {variantClass} {sizeClass} {icon ? 'btn-icon' : ''} {className}"
	disabled={disabled || loading}
	{...rest}
>
	{#if loading}
		<span class="spinner"></span>
	{/if}
	{@render children()}
</button>
