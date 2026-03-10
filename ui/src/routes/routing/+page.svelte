<script lang="ts">
	import { api } from '$lib/api';
	import { invalidateAll } from '$app/navigation';

	let { data } = $props<{ data: any }>();

	let showNewRoute = $state(false);
	let showNewCert = $state(false);
	let error = $state('');
	let success = $state('');
	let busy = $state('');
	let selectedProjectIdOverride = $state('');
	let selectedProjectId = $derived(selectedProjectIdOverride || data.projectId || '');
	let serviceOptions = $state<string[]>([]);

	$effect(() => {
		serviceOptions = data.serviceOptions ?? [];
	});

	let newRoute = $state({ name: '', domain: '', target_service: '', target_port: 80, ssl_mode: 'letsencrypt' });
	let newCert = $state({ domain: '', cert_pem: '', key_pem: '', is_wildcard: false });

	async function loadProjectRoutes() {
		if (!selectedProjectId) return;
		busy = 'load-project';
		try {
			const [routes, certs, services] = await Promise.all([
				api.listProxyRoutes(selectedProjectId),
				api.listCertificates(selectedProjectId),
				api.metricsServices().then((s) => s.map((x) => x.service_name).sort()).catch(() => serviceOptions),
			]);
			data.routes = routes;
			data.certs = certs;
			serviceOptions = services;
			success = 'Project routes refreshed.';
		} catch {}
		busy = '';
	}

	function isValidDomain(value: string): boolean {
		if (!value) return false;
		const v = value.trim();
		return /^(\*\.)?[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/.test(v);
	}

	async function createRoute() {
		if (!selectedProjectId) { error = 'Select a project first'; return; }
		if (!isValidDomain(newRoute.domain)) { error = 'Enter a valid domain (example.com or *.example.com)'; return; }
		if (!newRoute.target_service.trim()) { error = 'Select a target service'; return; }
		if (newRoute.target_port < 1 || newRoute.target_port > 65535) { error = 'Target port must be between 1 and 65535'; return; }
		if ((data.routes ?? []).some((r: any) => r.domain === newRoute.domain)) { error = 'A route already exists for this domain'; return; }
		busy = 'create-route';
		try {
			await api.createProxyRoute(selectedProjectId, newRoute);
			newRoute = { name: '', domain: '', target_service: '', target_port: 80, ssl_mode: 'letsencrypt' };
			showNewRoute = false;
			await invalidateAll();
			success = 'Route created.';
		} catch (e: any) {
			error = e.message;
		}
		busy = '';
	}

	async function deleteRoute(id: string) {
		if (!selectedProjectId) return;
		if (!confirm('Delete this route?')) return;
		busy = `delete-route:${id}`;
		try {
			await api.deleteProxyRoute(selectedProjectId, id);
			await invalidateAll();
			success = 'Route deleted.';
		} catch (e: any) {
			error = e.message;
		}
		busy = '';
	}

	async function createCert() {
		if (!selectedProjectId) { error = 'Select a project first'; return; }
		if (!isValidDomain(newCert.domain)) { error = 'Enter a valid certificate domain'; return; }
		if (!newCert.cert_pem.trim() || !newCert.key_pem.trim()) { error = 'Certificate and private key are required'; return; }
		busy = 'create-cert';
		try {
			await api.createCertificate(selectedProjectId, newCert);
			newCert = { domain: '', cert_pem: '', key_pem: '', is_wildcard: false };
			showNewCert = false;
			await invalidateAll();
			success = 'Certificate uploaded.';
		} catch (e: any) {
			error = e.message;
		}
		busy = '';
	}
</script>

<svelte:head><title>Routing | Hive</title></svelte:head>

<div class="max-w-6xl mx-auto">
	<h2 class="text-2xl font-bold mb-6">Routing & Certificates</h2>

	<!-- Active Traefik Routes (from Swarm service labels) -->
	{#if data.activeRoutes && data.activeRoutes.length > 0}
		<div class="mb-8">
			<h3 class="text-lg font-semibold mb-4">Active Traefik Routes</h3>
			<p class="text-xs mb-3" style="color: var(--color-text-muted);">Routes discovered from Docker Swarm service labels</p>
			<div class="rounded-lg overflow-hidden" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<table class="w-full text-sm">
					<thead>
						<tr style="border-bottom: 1px solid var(--color-border);">
							<th class="text-left p-3 font-medium" style="color: var(--color-text-muted);">Service</th>
							<th class="text-left p-3 font-medium" style="color: var(--color-text-muted);">Rule</th>
							<th class="text-left p-3 font-medium" style="color: var(--color-text-muted);">Entrypoint</th>
							<th class="text-left p-3 font-medium" style="color: var(--color-text-muted);">SSL</th>
							<th class="text-left p-3 font-medium" style="color: var(--color-text-muted);">Port</th>
						</tr>
					</thead>
					<tbody>
						{#each data.activeRoutes as route}
							<tr style="border-bottom: 1px solid var(--color-border);">
								<td class="p-3 font-mono text-xs">{route.service}</td>
								<td class="p-3 text-xs">{route.domain}</td>
								<td class="p-3 text-xs">{route.entrypoint || 'default'}</td>
								<td class="p-3">
									{#if route.cert_resolver}
										<span class="px-2 py-0.5 rounded text-xs" style="background-color: rgba(34, 197, 94, 0.15); color: var(--color-success);">{route.cert_resolver}</span>
									{:else}
										<span class="text-xs" style="color: var(--color-text-muted);">none</span>
									{/if}
								</td>
								<td class="p-3 font-mono text-xs">{route.port || '--'}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</div>
	{/if}

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);">
			<p style="color: var(--color-danger);">{error}</p>
		</div>
	{/if}
	{#if success}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(34, 197, 94, 0.1); border: 1px solid var(--color-success);">
			<p style="color: var(--color-success);">{success}</p>
		</div>
	{/if}

	<div class="mb-8">
		<div class="flex items-center justify-between mb-4">
			<div class="flex items-center gap-3">
				<h3 class="text-lg font-semibold">Proxy Routes</h3>
				{#if data.projects && data.projects.length > 1}
					<select
						bind:value={selectedProjectIdOverride}
						onchange={() => loadProjectRoutes()}
						class="px-2 py-1 rounded text-xs"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
					>
						{#each data.projects as project}
							<option value={project.id}>{project.name}</option>
						{/each}
					</select>
				{/if}
				<button
					onclick={loadProjectRoutes}
					class="px-2 py-1 rounded text-xs"
					disabled={busy === 'load-project'}
					style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
				>
					{busy === 'load-project' ? 'Refreshing...' : 'Refresh'}
				</button>
			</div>
			<button onclick={() => showNewRoute = !showNewRoute} class="px-4 py-2 rounded-lg text-sm font-medium" style="background-color: var(--color-primary); color: var(--color-bg);">
				{showNewRoute ? 'Cancel' : 'New Route'}
			</button>
		</div>

		{#if showNewRoute}
			<div class="rounded-lg p-4 mb-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<div class="grid grid-cols-2 gap-4 mb-4">
					<input bind:value={newRoute.name} placeholder="Route name" class="px-3 py-2 rounded-lg text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
					<input bind:value={newRoute.domain} placeholder="Domain (e.g. app.example.com)" class="px-3 py-2 rounded-lg text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
					<select bind:value={newRoute.target_service} class="px-3 py-2 rounded-lg text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);">
						<option value="">Select target service</option>
						{#each serviceOptions as svc}
							<option value={svc}>{svc}</option>
						{/each}
					</select>
					<input type="number" bind:value={newRoute.target_port} placeholder="Port" class="px-3 py-2 rounded-lg text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
					<select bind:value={newRoute.ssl_mode} class="px-3 py-2 rounded-lg text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);">
						<option value="letsencrypt">Let's Encrypt</option>
						<option value="cloudflare">Cloudflare DNS-01</option>
						<option value="custom">Custom Certificate</option>
					</select>
				</div>
				<p class="text-xs" style="color: var(--color-text-muted);">Use app domain for simple one-service routing; use proxy routes for custom service-to-domain mappings.</p>
				<button onclick={createRoute} disabled={busy === 'create-route'} class="px-4 py-2 rounded-lg text-sm font-medium" style="background-color: var(--color-success); color: white;">
					{busy === 'create-route' ? 'Creating...' : 'Create Route'}
				</button>
			</div>
		{/if}

		<div class="rounded-lg overflow-hidden" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<table class="w-full text-sm">
				<thead>
					<tr style="border-bottom: 1px solid var(--color-border);">
						<th class="text-left p-3 font-medium" style="color: var(--color-text-muted);">Name</th>
						<th class="text-left p-3 font-medium" style="color: var(--color-text-muted);">Domain</th>
						<th class="text-left p-3 font-medium" style="color: var(--color-text-muted);">Target</th>
						<th class="text-left p-3 font-medium" style="color: var(--color-text-muted);">SSL</th>
						<th class="text-left p-3 font-medium" style="color: var(--color-text-muted);">Status</th>
						<th class="p-3"></th>
					</tr>
				</thead>
				<tbody>
					{#each data.routes ?? [] as route}
						<tr style="border-bottom: 1px solid var(--color-border);">
							<td class="p-3 font-medium">{route.name}</td>
							<td class="p-3">{route.domain}</td>
							<td class="p-3" style="color: var(--color-text-muted);">{route.target_service}:{route.target_port}</td>
							<td class="p-3"><span class="px-2 py-0.5 rounded text-xs" style="background-color: rgba(212, 168, 67, 0.1); color: var(--color-info);">{route.ssl_mode}</span></td>
							<td class="p-3"><span class="inline-block w-2 h-2 rounded-full" style="background-color: {route.enabled ? 'var(--color-success)' : 'var(--color-danger)'};"></span></td>
							<td class="p-3">
								<button onclick={() => deleteRoute(route.id)} class="text-xs" style="color: var(--color-danger);" disabled={busy === `delete-route:${route.id}`}>
									{busy === `delete-route:${route.id}` ? 'Deleting...' : 'Delete'}
								</button>
							</td>
						</tr>
					{:else}
						<tr><td colspan="6" class="p-4 text-center" style="color: var(--color-text-muted);">No routes configured</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
	</div>

	<div>
		<div class="flex items-center justify-between mb-4">
			<h3 class="text-lg font-semibold">Custom Certificates</h3>
			<button onclick={() => showNewCert = !showNewCert} class="px-4 py-2 rounded-lg text-sm font-medium" style="background-color: var(--color-primary); color: var(--color-bg);">
				{showNewCert ? 'Cancel' : 'Upload Certificate'}
			</button>
		</div>

		{#if showNewCert}
			<div class="rounded-lg p-4 mb-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<div class="space-y-4 mb-4">
					<input bind:value={newCert.domain} placeholder="Domain (e.g. *.example.com)" class="w-full px-3 py-2 rounded-lg text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
					<textarea bind:value={newCert.cert_pem} placeholder="Certificate PEM" rows="4" class="w-full px-3 py-2 rounded-lg text-sm font-mono" style="background-color: var(--color-bg); border: 1px solid var(--color-border);"></textarea>
					<textarea bind:value={newCert.key_pem} placeholder="Private Key PEM" rows="4" class="w-full px-3 py-2 rounded-lg text-sm font-mono" style="background-color: var(--color-bg); border: 1px solid var(--color-border);"></textarea>
					<label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={newCert.is_wildcard} /> Wildcard certificate</label>
				</div>
				<button onclick={createCert} disabled={busy === 'create-cert'} class="px-4 py-2 rounded-lg text-sm font-medium" style="background-color: var(--color-success); color: white;">
					{busy === 'create-cert' ? 'Uploading...' : 'Upload'}
				</button>
			</div>
		{/if}

		<div class="grid grid-cols-1 md:grid-cols-2 gap-3">
			{#each data.certs ?? [] as cert}
				<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
					<div class="flex items-center justify-between">
						<div>
							<p class="font-medium">{cert.domain}</p>
							<p class="text-xs" style="color: var(--color-text-muted);">{cert.provider}{cert.is_wildcard ? ' (wildcard)' : ''}</p>
						</div>
					</div>
				</div>
			{:else}
				<p class="text-sm" style="color: var(--color-text-muted);">No custom certificates uploaded</p>
			{/each}
		</div>
	</div>
</div>
