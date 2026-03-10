<script lang="ts">
	let {
		data = [],
		width = 120,
		height = 32,
		color = 'var(--color-success)',
		fillOpacity = 0.15,
		strokeWidth = 1.5,
		showArea = true,
		min: customMin,
		max: customMax
	}: {
		data: number[];
		width?: number;
		height?: number;
		color?: string;
		fillOpacity?: number;
		strokeWidth?: number;
		showArea?: boolean;
		min?: number;
		max?: number;
	} = $props();

	let path = $derived.by(() => {
		if (data.length < 2) return '';
		const minVal = customMin ?? Math.min(...data);
		const maxVal = customMax ?? Math.max(...data, 1);
		const range = maxVal - minVal || 1;
		const pad = 1;
		const w = width - pad * 2;
		const h = height - pad * 2;
		const step = w / (data.length - 1);

		return data.map((v, i) => {
			const x = pad + i * step;
			const y = pad + h - ((v - minVal) / range) * h;
			return `${i === 0 ? 'M' : 'L'} ${x.toFixed(1)} ${y.toFixed(1)}`;
		}).join(' ');
	});

	let areaPath = $derived.by(() => {
		if (!showArea || data.length < 2) return '';
		const pad = 1;
		const w = width - pad * 2;
		return `${path} L ${(pad + w).toFixed(1)} ${height - pad} L ${pad} ${height - pad} Z`;
	});
</script>

<svg viewBox="0 0 {width} {height}" preserveAspectRatio="none" class="sparkline" style="height: {height}px;">
	{#if areaPath}
		<path d={areaPath} fill={color} opacity={fillOpacity} />
	{/if}
	{#if path}
		<path d={path} fill="none" stroke={color} stroke-width={strokeWidth} stroke-linecap="round" stroke-linejoin="round" />
	{/if}
</svg>

<style>
	.sparkline {
		display: block;
		width: 100%;
		max-width: 100%;
	}
</style>
