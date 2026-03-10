<script lang="ts">
	interface Crumb {
		label: string;
		href?: string;
	}

	interface Props {
		items: Crumb[];
	}

	let { items }: Props = $props();
</script>

<nav class="breadcrumbs" aria-label="Breadcrumb">
	{#each items as crumb, i}
		{#if i > 0}
			<span class="separator">/</span>
		{/if}
		{#if crumb.href && i < items.length - 1}
			<a href={crumb.href}>{crumb.label}</a>
		{:else}
			<span class="current">{crumb.label}</span>
		{/if}
	{/each}
</nav>
