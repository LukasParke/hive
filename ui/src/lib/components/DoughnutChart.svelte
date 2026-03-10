<script lang="ts">
	interface Segment {
		value: number;
		label: string;
		color: string;
	}

	interface Props {
		segments: Segment[];
		size?: number;
		strokeWidth?: number;
		centerLabel?: string;
		centerValue?: string;
	}

	let {
		segments = [],
		size = 120,
		strokeWidth = 12,
		centerLabel = '',
		centerValue = ''
	}: Props = $props();

	let radius = $derived((size - strokeWidth) / 2);
	let circumference = $derived(2 * Math.PI * radius);
	let total = $derived(segments.reduce((sum, s) => sum + s.value, 0) || 1);

	let arcs = $derived.by(() => {
		let offset = 0;
		return segments.map(s => {
			const pct = s.value / total;
			const dashLength = pct * circumference;
			const dashOffset = -offset * circumference;
			offset += pct;
			return { ...s, dashLength, dashOffset, pct };
		});
	});
</script>

<div class="doughnut-chart" style="width: {size}px; height: {size}px;">
	<svg width={size} height={size} viewBox="0 0 {size} {size}">
		<circle
			cx={size / 2} cy={size / 2} r={radius}
			fill="none" stroke="var(--color-border)" stroke-width={strokeWidth}
		/>
		{#each arcs as arc}
			<circle
				cx={size / 2} cy={size / 2} r={radius}
				fill="none" stroke={arc.color} stroke-width={strokeWidth}
				stroke-dasharray="{arc.dashLength} {circumference}"
				stroke-dashoffset={arc.dashOffset}
				stroke-linecap="round"
				transform="rotate(-90 {size / 2} {size / 2})"
				class="arc-segment"
			/>
		{/each}
	</svg>
	{#if centerLabel || centerValue}
		<div class="center-text">
			{#if centerValue}<span class="center-value">{centerValue}</span>{/if}
			{#if centerLabel}<span class="center-label">{centerLabel}</span>{/if}
		</div>
	{/if}
</div>

<style>
	.doughnut-chart {
		position: relative;
		display: inline-flex;
		align-items: center;
		justify-content: center;
	}
	.center-text {
		position: absolute;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 0.125rem;
	}
	.center-value {
		font-size: 1.125rem;
		font-weight: 700;
		font-variant-numeric: tabular-nums;
	}
	.center-label {
		font-size: 0.625rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--color-text-muted);
	}
	.arc-segment {
		transition: stroke-dasharray 0.5s ease, stroke-dashoffset 0.5s ease;
	}
</style>
