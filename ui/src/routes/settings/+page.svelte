<script lang="ts">
	import { Card } from '$lib/components';

	let { data } = $props();
</script>

<svelte:head><title>Settings | Hive</title></svelte:head>

<div class="max-w-3xl mx-auto">
	<div class="page-header">
		<div>
			<h2 class="page-title">Settings</h2>
			<p class="page-subtitle">Account information and preferences</p>
		</div>
	</div>

	{#if data.user}
		<Card>
			{#snippet header()}
				<h3 style="font-weight: 600">Account</h3>
			{/snippet}
			<div class="grid grid-cols-2 gap-4 text-sm">
				<div>
					<span class="text-muted">Name</span>
					<p style="font-weight: 500; margin-top: 2px">{data.user.name || '--'}</p>
				</div>
				<div>
					<span class="text-muted">Email</span>
					<p style="font-weight: 500; margin-top: 2px">{data.user.email}</p>
				</div>
			</div>
		</Card>
	{/if}
</div>
