<script lang="ts">
	import { api, type ProxyRoute, type CustomCertificate } from '$lib/api';
	import { onMount } from 'svelte';

	let routes = $state<ProxyRoute[]>([]);
	let certs = $state<CustomCertificate[]>([]);
	let showNewRoute = $state(false);
	let showNewCert = $state(false);
	let projectId = $state('');
	let error = $state('');

	let newRoute = $state({ name: '', domain: '', target_service: '', target_port: 80, ssl_mode: 'letsencrypt' });
	let newCert = $state({ domain: '', cert_pem: '', key_pem: '', is_wildcard: false });

	onMount(async () => {
		const projects = await api.listProjects();
		if (projects.length > 0) {
			projectId = projects[0].id;
			await loadData();
		}
	});

	async function loadData() {
		if (!projectId) return;
		try {
			[routes, certs] = await Promise.all([
				api.listProxyRoutes(projectId),
				api.listCertificates(projectId),
			]);
		} catch (e: any) {
			error = e.message;
		}
	}

	async function createRoute() {
		try {
			await api.createProxyRoute(projectId, newRoute);
			newRoute = { name: '', domain: '', target_service: '', target_port: 80, ssl_mode: 'letsencrypt' };
			showNewRoute = false;
			await loadData();
		} catch (e: any) {
			error = e.message;
		}
	}

	async function deleteRoute(id: string) {
		try {
			await api.deleteProxyRoute(projectId, id);
			await loadData();
		} catch (e: any) {
			error = e.message;
		}
	}

	async function createCert() {
		try {
			await api.createCertificate(projectId, newCert);
			newCert = { domain: '', cert_pem: '', key_pem: '', is_wildcard: false };
			showNewCert = false;
			await loadData();
		} catch (e: any) {
			error = e.message;
		}
	}
</script>

<div class="max-w-6xl mx-auto">
	<h2 class="text-2xl font-bold mb-6">Routing & Certificates</h2>

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid #ef4444;">
			<p style="color: #ef4444;">{error}</p>
		</div>
	{/if}

	<div class="mb-8">
		<div class="flex items-center justify-between mb-4">
			<h3 class="text-lg font-semibold">Proxy Routes</h3>
			<button onclick={() => showNewRoute = !showNewRoute} class="px-4 py-2 rounded-lg text-sm font-medium" style="background-color: #3b82f6; color: white;">
				{showNewRoute ? 'Cancel' : 'New Route'}
			</button>
		</div>

		{#if showNewRoute}
			<div class="rounded-lg p-4 mb-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<div class="grid grid-cols-2 gap-4 mb-4">
					<input bind:value={newRoute.name} placeholder="Route name" class="px-3 py-2 rounded-lg text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
					<input bind:value={newRoute.domain} placeholder="Domain (e.g. app.example.com)" class="px-3 py-2 rounded-lg text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
					<input bind:value={newRoute.target_service} placeholder="Target service name" class="px-3 py-2 rounded-lg text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
					<input type="number" bind:value={newRoute.target_port} placeholder="Port" class="px-3 py-2 rounded-lg text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
					<select bind:value={newRoute.ssl_mode} class="px-3 py-2 rounded-lg text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);">
						<option value="letsencrypt">Let's Encrypt</option>
						<option value="cloudflare">Cloudflare DNS-01</option>
						<option value="custom">Custom Certificate</option>
					</select>
				</div>
				<button onclick={createRoute} class="px-4 py-2 rounded-lg text-sm font-medium" style="background-color: #22c55e; color: white;">Create Route</button>
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
					{#each routes as route}
						<tr style="border-bottom: 1px solid var(--color-border);">
							<td class="p-3 font-medium">{route.name}</td>
							<td class="p-3">{route.domain}</td>
							<td class="p-3" style="color: var(--color-text-muted);">{route.target_service}:{route.target_port}</td>
							<td class="p-3"><span class="px-2 py-0.5 rounded text-xs" style="background-color: rgba(59, 130, 246, 0.15); color: #3b82f6;">{route.ssl_mode}</span></td>
							<td class="p-3"><span class="inline-block w-2 h-2 rounded-full" style="background-color: {route.enabled ? '#22c55e' : '#ef4444'};"></span></td>
							<td class="p-3"><button onclick={() => deleteRoute(route.id)} class="text-xs" style="color: #ef4444;">Delete</button></td>
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
			<button onclick={() => showNewCert = !showNewCert} class="px-4 py-2 rounded-lg text-sm font-medium" style="background-color: #3b82f6; color: white;">
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
				<button onclick={createCert} class="px-4 py-2 rounded-lg text-sm font-medium" style="background-color: #22c55e; color: white;">Upload</button>
			</div>
		{/if}

		<div class="grid grid-cols-1 md:grid-cols-2 gap-3">
			{#each certs as cert}
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
