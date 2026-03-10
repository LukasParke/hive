<script lang="ts">
	let {
		value = 0,
		max = 100,
		height = 6,
		class: className = '',
	}: {
		value: number;
		max?: number;
		height?: number;
		class?: string;
	} = $props();

	let pct = $derived(max > 0 ? Math.min((value / max) * 100, 100) : 0);
	let color = $derived(
		pct > 85 ? 'var(--color-danger)' :
		pct > 60 ? 'var(--color-primary)' :
		'var(--color-success)'
	);
</script>

<div class="progress-track {className}" style="height: {height}px;">
	<div class="progress-fill" style="width: {pct}%; background-color: {color};"></div>
</div>

<style>
	.progress-track {
		width: 100%;
		border-radius: 9999px;
		background-color: var(--color-border);
	}
	.progress-fill {
		height: 100%;
		border-radius: 9999px;
		transition: width 0.6s cubic-bezier(0.4, 0, 0.2, 1), background-color 0.3s ease;
	}
</style>
