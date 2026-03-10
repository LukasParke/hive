<script lang="ts">
	import { api } from '$lib/api';
	import { Button } from '$lib/components';

	let { data } = $props();

	type Summary = { critical: number; high: number; medium: number; low: number };
	let summary = $derived<Summary>(data.summary ?? { critical: 0, high: 0, medium: 0, low: 0 });
	let scanLoading = $state(false);

	function handleScanAll() {
		scanLoading = true;
		// Placeholder: per-app scans are done from app Security tab
		setTimeout(() => {
			scanLoading = false;
		}, 500);
	}

	const total = $derived(summary.critical + summary.high + summary.medium + summary.low);
	const cards = $derived([
		{ label: 'Critical', value: summary.critical, valueClass: 'text-red-400' },
		{ label: 'High', value: summary.high, valueClass: 'text-orange-400' },
		{ label: 'Medium', value: summary.medium, valueClass: 'text-amber-400' },
		{ label: 'Low', value: summary.low, valueClass: 'text-slate-400' }
	]);
</script>

<svelte:head><title>Security | Hive</title></svelte:head>

<div class="max-w-4xl mx-auto">
	<div class="page-header">
		<div>
			<h2 class="page-title">Security</h2>
			<p class="page-subtitle">Vulnerability overview across all apps</p>
		</div>
		<Button variant="secondary" onclick={handleScanAll} loading={scanLoading} disabled>
			Scan All (coming soon)
		</Button>
	</div>

	<div class="mb-6 p-4 rounded-lg bg-slate-800/50 border border-slate-700">
		<p class="text-sm text-slate-400 leading-relaxed">
			Vulnerability scans are run per app from the <strong class="text-slate-300">Security</strong> tab on each app page.
			Use that tab to trigger scans and view detailed vulnerability reports for container images.
		</p>
	</div>

	<div class="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
		{#each cards as card}
			<div class="hive-card hive-card-body">
				<div class="flex flex-col items-center justify-center text-center">
					<span class="text-3xl font-bold {card.valueClass}">{card.value}</span>
					<span class="text-sm text-slate-400 mt-1">{card.label}</span>
				</div>
			</div>
		{/each}
	</div>

	<div class="hive-card hive-card-body">
		<h3 class="text-sm font-semibold text-slate-300 mb-2">Summary</h3>
		<p class="text-sm text-slate-400">
			{total === 0
				? 'No vulnerabilities detected. Run scans on individual apps from their Security tab to check for updates.'
				: `${total} total vulnerabilit${total === 1 ? 'y' : 'ies'} across scanned apps. Address critical and high severity issues first.`}
		</p>
	</div>
</div>
