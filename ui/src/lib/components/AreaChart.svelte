<script lang="ts">
	interface Props {
		data: { ts: number; value: number }[];
		width?: number;
		height?: number;
		color?: string;
		fillOpacity?: number;
		strokeWidth?: number;
		label?: string;
		unit?: string;
		min?: number;
		max?: number;
		showGrid?: boolean;
		showLabels?: boolean;
	}

	let {
		data = [],
		width = 600,
		height = 120,
		color = 'var(--color-success)',
		fillOpacity = 0.15,
		strokeWidth = 1.5,
		label = '',
		unit = '',
		min: customMin,
		max: customMax,
		showGrid = true,
		showLabels = true,
	}: Props = $props();

	const pad = { top: 8, right: 12, bottom: showLabels ? 24 : 8, left: showLabels ? 40 : 8 };

	let values = $derived(data.map(d => d.value));
	let minVal = $derived(customMin ?? Math.min(...(values.length ? values : [0])));
	let maxVal = $derived(customMax ?? Math.max(...(values.length ? values : [1]), minVal + 1));
	let range = $derived(maxVal - minVal || 1);

	let chartW = $derived(width - pad.left - pad.right);
	let chartH = $derived(height - pad.top - pad.bottom);

	let linePath = $derived.by(() => {
		if (values.length < 2) return '';
		const step = chartW / (values.length - 1);
		return values.map((v, i) => {
			const x = pad.left + i * step;
			const y = pad.top + chartH - ((v - minVal) / range) * chartH;
			return `${i === 0 ? 'M' : 'L'} ${x.toFixed(1)} ${y.toFixed(1)}`;
		}).join(' ');
	});

	let areaPath = $derived.by(() => {
		if (values.length < 2 || !linePath) return '';
		const step = chartW / (values.length - 1);
		const lastX = pad.left + (values.length - 1) * step;
		const baseline = pad.top + chartH;
		return `${linePath} L ${lastX.toFixed(1)} ${baseline} L ${pad.left} ${baseline} Z`;
	});

	let gridLines = $derived.by(() => {
		if (!showGrid) return [];
		const count = 4;
		return Array.from({ length: count + 1 }, (_, i) => {
			const val = minVal + (range / count) * i;
			const y = pad.top + chartH - ((val - minVal) / range) * chartH;
			return { y, label: val >= 1000 ? `${(val / 1024 / 1024 / 1024).toFixed(0)}G` : val.toFixed(0) };
		});
	});

	let timeLabels = $derived.by(() => {
		if (!showLabels || data.length < 2) return [];
		const count = Math.min(5, data.length);
		const step = Math.floor((data.length - 1) / (count - 1));
		return Array.from({ length: count }, (_, i) => {
			const idx = Math.min(i * step, data.length - 1);
			const x = pad.left + (idx / (data.length - 1)) * chartW;
			const d = new Date(data[idx].ts * 1000);
			return { x, label: `${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}` };
		});
	});

	let currentValue = $derived(values.length > 0 ? values[values.length - 1] : 0);
</script>

<div class="area-chart">
	{#if label}
		<div class="chart-header">
			<span class="chart-label">{label}</span>
			<span class="chart-value">{currentValue.toFixed(1)}{unit}</span>
		</div>
	{/if}
	<svg width="100%" viewBox="0 0 {width} {height}" preserveAspectRatio="none" class="chart-svg">
		{#if showGrid}
			{#each gridLines as line}
				<line x1={pad.left} y1={line.y} x2={pad.left + chartW} y2={line.y}
					stroke="var(--color-border)" stroke-width="0.5" stroke-dasharray="4 4" />
				{#if showLabels}
					<text x={pad.left - 6} y={line.y + 3} text-anchor="end"
						fill="var(--color-text-muted)" font-size="9" font-family="monospace">{line.label}{unit}</text>
				{/if}
			{/each}
		{/if}

		{#if showLabels}
			{#each timeLabels as tl}
				<text x={tl.x} y={height - 4} text-anchor="middle"
					fill="var(--color-text-muted)" font-size="9" font-family="monospace">{tl.label}</text>
			{/each}
		{/if}

		{#if areaPath}
			<path d={areaPath} fill={color} opacity={fillOpacity} />
		{/if}
		{#if linePath}
			<path d={linePath} fill="none" stroke={color} stroke-width={strokeWidth}
				stroke-linecap="round" stroke-linejoin="round" />
		{/if}
	</svg>
</div>

<style>
	.area-chart {
		width: 100%;
	}
	.chart-header {
		display: flex;
		justify-content: space-between;
		align-items: baseline;
		margin-bottom: 0.25rem;
		padding: 0 0.25rem;
	}
	.chart-label {
		font-size: 0.6875rem;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--color-text-muted);
	}
	.chart-value {
		font-size: 0.8125rem;
		font-weight: 600;
		font-variant-numeric: tabular-nums;
	}
	.chart-svg {
		display: block;
	}
</style>
