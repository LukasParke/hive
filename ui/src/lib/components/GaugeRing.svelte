<script lang="ts">
	let {
		value = 0,
		size = 64,
		strokeWidth = 6,
		label = '',
		format = (v: number) => `${v.toFixed(0)}%`
	}: {
		value: number;
		size?: number;
		strokeWidth?: number;
		label?: string;
		format?: (v: number) => string;
	} = $props();

	let radius = $derived((size - strokeWidth) / 2);
	let circumference = $derived(2 * Math.PI * radius);
	let offset = $derived(circumference - (Math.min(Math.max(value, 0), 100) / 100) * circumference);

	let color = $derived(
		value > 85 ? 'var(--color-danger)' :
		value > 60 ? 'var(--color-primary)' :
		'var(--color-success)'
	);
</script>

<div class="gauge" style="width: {size}px; height: {size}px;">
	<svg width={size} height={size} viewBox="0 0 {size} {size}">
		<circle
			cx={size / 2}
			cy={size / 2}
			r={radius}
			fill="none"
			stroke="var(--color-border)"
			stroke-width={strokeWidth}
		/>
		<circle
			cx={size / 2}
			cy={size / 2}
			r={radius}
			fill="none"
			stroke={color}
			stroke-width={strokeWidth}
			stroke-dasharray={circumference}
			stroke-dashoffset={offset}
			stroke-linecap="round"
			transform="rotate(-90 {size / 2} {size / 2})"
			class="gauge-fill"
		/>
	</svg>
	<div class="gauge-label">
		<span class="gauge-value" style="color: {color};">{format(value)}</span>
		{#if label}
			<span class="gauge-text">{label}</span>
		{/if}
	</div>
</div>

<style>
	.gauge {
		position: relative;
		display: inline-flex;
		align-items: center;
		justify-content: center;
	}
	.gauge-fill {
		transition: stroke-dashoffset 0.6s cubic-bezier(0.4, 0, 0.2, 1), stroke 0.3s ease;
	}
	.gauge-label {
		position: absolute;
		display: flex;
		flex-direction: column;
		align-items: center;
		line-height: 1;
	}
	.gauge-value {
		font-weight: 700;
		font-size: 0.75rem;
		font-variant-numeric: tabular-nums;
	}
	.gauge-text {
		font-size: 0.5rem;
		color: var(--color-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		margin-top: 1px;
	}
</style>
