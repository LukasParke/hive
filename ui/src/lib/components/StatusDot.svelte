<script lang="ts">
	let {
		status = 'neutral',
		pulse = false,
		size = 8,
	}: {
		status?: 'success' | 'danger' | 'warning' | 'info' | 'neutral';
		pulse?: boolean;
		size?: number;
	} = $props();

	const colorMap: Record<string, string> = {
		success: 'var(--color-success)',
		danger: 'var(--color-danger)',
		warning: 'var(--color-warning)',
		info: 'var(--color-info)',
		neutral: 'var(--color-text-muted)',
	};
</script>

<span
	class="status-dot"
	class:pulse
	style="width: {size}px; height: {size}px; background-color: {colorMap[status]};"
></span>

<style>
	.status-dot {
		display: inline-block;
		border-radius: 50%;
		flex-shrink: 0;
	}
	.pulse {
		animation: pulse-dot 2s ease-in-out infinite;
	}
	@keyframes pulse-dot {
		0%, 100% { opacity: 1; }
		50% { opacity: 0.4; }
	}
</style>
