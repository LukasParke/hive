<script lang="ts">
	let { size = 32 }: { size?: number } = $props();
</script>

<svg width={size} height={size} viewBox="0 0 100 100" fill="none" xmlns="http://www.w3.org/2000/svg">
	<!-- Top hexagon -->
	<polygon
		points="50,8 68,18 68,38 50,48 32,38 32,18"
		fill="var(--color-primary)"
		opacity="1"
	/>
	<!-- Bottom-left hexagon -->
	<polygon
		points="30,42 48,52 48,72 30,82 12,72 12,52"
		fill="var(--color-primary)"
		opacity="0.6"
	/>
	<!-- Bottom-right hexagon -->
	<polygon
		points="70,42 88,52 88,72 70,82 52,72 52,52"
		fill="var(--color-primary)"
		opacity="0.6"
	/>
</svg>
