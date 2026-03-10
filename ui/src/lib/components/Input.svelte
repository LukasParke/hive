<script lang="ts">
	import type { HTMLInputAttributes } from 'svelte/elements';

	interface Props extends HTMLInputAttributes {
		label?: string;
		error?: string;
		hint?: string;
		value?: string | number;
	}

	let {
		label,
		error,
		hint,
		id,
		value = $bindable(''),
		class: className = '',
		...rest
	}: Props = $props();

	const inputId = $derived(id || label?.toLowerCase().replace(/\s+/g, '-'));
</script>

<div class="flex flex-col gap-1">
	{#if label}
		<label for={inputId}>{label}</label>
	{/if}
	<input
		id={inputId}
		class={className}
		style:border-color={error ? 'var(--color-danger)' : undefined}
		bind:value
		{...rest}
	/>
	{#if error}
		<span class="text-xs" style="color: var(--color-danger)">{error}</span>
	{:else if hint}
		<span class="text-xs text-muted">{hint}</span>
	{/if}
</div>
