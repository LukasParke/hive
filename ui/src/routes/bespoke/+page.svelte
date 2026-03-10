<script lang="ts">
	import type { BespokeAppClass } from '$lib/types';
	import { Badge, Card, EmptyState } from '$lib/components';

	let { data } = $props();
	const apps = $derived((data.apps ?? []) as BespokeAppClass[]);
</script>

<svelte:head>
	<title>Bespoke Apps | Hive</title>
</svelte:head>

<div class="max-w-7xl mx-auto">
	<div class="page-header">
		<div>
			<h1 class="page-title">Bespoke App Profiles</h1>
			<p class="page-subtitle">Guided deployment profiles for high-value homelab applications</p>
		</div>
	</div>

	{#if apps.length === 0}
		<EmptyState
			title="No bespoke profiles yet"
			description="Profiles will appear here as Hive ships curated app experiences."
		/>
	{:else}
		<div class="grid">
			{#each apps as app (app.slug)}
				<Card>
					<div class="head">
						<div>
							<h3 class="title">{app.name}</h3>
							<p class="desc">{app.description}</p>
						</div>
						<Badge variant={app.template_available ? 'success' : 'warning'}>
							{app.template_available ? 'template ready' : 'template missing'}
						</Badge>
					</div>

					<div class="meta">
						<div><span>Profile:</span> <code>{app.slug}</code></div>
						<div><span>Base template:</span> <code>{app.template_name}</code></div>
						<div><span>Category:</span> {app.category}</div>
					</div>

					{#if app.recommended_ports.length > 0}
						<div class="section">
							<div class="section-title">Recommended Ports</div>
							<div class="chips">
								{#each app.recommended_ports as p}
									<span class="chip">{p}</span>
								{/each}
							</div>
						</div>
					{/if}

					{#if Object.keys(app.recommended_env || {}).length > 0}
						<div class="section">
							<div class="section-title">Recommended Environment</div>
							<div class="kv">
								{#each Object.entries(app.recommended_env) as [k, v]}
									<div class="kv-row">
										<code>{k}</code>
										<span>{v || '(set in deploy form)'}</span>
									</div>
								{/each}
							</div>
						</div>
					{/if}

					{#if app.notes.length > 0}
						<div class="section">
							<div class="section-title">Operational Notes</div>
							<ul class="notes">
								{#each app.notes as note}
									<li>{note}</li>
								{/each}
							</ul>
						</div>
					{/if}

					<div class="actions">
						<a class="btn btn-primary btn-sm" href="/catalog">Deploy via Catalog</a>
						<a class="btn btn-ghost btn-sm" href="/operations">View Operations</a>
					</div>
				</Card>
			{/each}
		</div>
	{/if}
</div>

<style>
	.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(340px, 1fr)); gap: 12px; }
	.head { display: flex; justify-content: space-between; gap: 10px; align-items: flex-start; margin-bottom: 8px; }
	.title { font-size: 1rem; font-weight: 700; margin: 0; }
	.desc { font-size: 0.86rem; color: var(--color-text-muted); margin-top: 2px; }
	.meta { display: grid; gap: 4px; font-size: 0.82rem; color: var(--color-text-muted); margin-bottom: 10px; }
	.meta span { color: var(--color-text); font-weight: 600; }
	.section { margin-bottom: 10px; }
	.section-title { font-size: 0.8rem; color: var(--color-text-muted); margin-bottom: 6px; text-transform: uppercase; letter-spacing: 0.04em; }
	.chips { display: flex; flex-wrap: wrap; gap: 6px; }
	.chip { border: 1px solid var(--color-border); border-radius: 999px; padding: 2px 8px; font-size: 0.78rem; }
	.kv { display: grid; gap: 6px; }
	.kv-row { display: flex; justify-content: space-between; gap: 8px; font-size: 0.8rem; }
	.notes { margin: 0; padding-left: 16px; color: var(--color-text-muted); font-size: 0.84rem; }
	.actions { display: flex; gap: 8px; margin-top: 8px; }
</style>
