<script lang="ts">
	import { api } from '$lib/api';
	import type { ConnectivityResult } from '$lib/types';

	let { data } = $props();

	let ingressMode = $state('port_forward');
	let cfApiToken = $state('');
	let cfTunnelToken = $state('');
	let connectivity = $state<ConnectivityResult | null>(null);
	let saving = $state(false);
	let tunnelRunning = $state(false);
	let tunnelTokenMasked = $state('');
	let cfApiTokenMasked = $state('');
	let tunnelCNAME = $state('');
	let error = $state('');
	let success = $state('');
	let tunnelStep = $state(1);
	let hydrated = $state(false);
	let initialIngressMode = $state('port_forward');
	const ingressDirty = $derived(initialIngressMode !== ingressMode);

	$effect(() => {
		if (hydrated) return;
		const settings = (data.settings ?? {}) as Record<string, any>;
		connectivity = (data.connectivity ?? null) as ConnectivityResult | null;
		ingressMode = (settings.ingress_mode as string) || 'port_forward';
		initialIngressMode = ingressMode;
		tunnelRunning = (settings.tunnel_running as boolean) || false;
		tunnelTokenMasked = (settings.tunnel_token as string) || '';
		cfApiTokenMasked = (settings.cf_api_token as string) || '';
		tunnelCNAME = (settings.tunnel_cname_target as string) || '';
		tunnelStep = tunnelRunning ? 4 : tunnelTokenMasked ? 3 : 1;
		hydrated = true;
	});

	async function refreshSettings() {
		try {
			const settings = await api.getNetworkingSettings();
			ingressMode = settings.ingress_mode || 'port_forward';
			initialIngressMode = ingressMode;
			tunnelRunning = settings.tunnel_running || false;
			tunnelTokenMasked = settings.tunnel_token || '';
			cfApiTokenMasked = settings.cf_api_token || '';
			tunnelCNAME = settings.tunnel_cname_target || '';
			if (tunnelRunning) tunnelStep = 4;
			else if (tunnelTokenMasked) tunnelStep = 3;
		} catch {}
	}

	async function checkConnectivity() {
		try {
			connectivity = await api.checkConnectivity();
		} catch (e: any) {
			error = e.message;
		}
	}

	async function saveSettings() {
		saving = true;
		error = '';
		success = '';
		try {
			await api.put('/networking', {
				ingress_mode: ingressMode,
				tunnel_token: cfTunnelToken || undefined,
				cf_api_token: cfApiToken || undefined,
			});
			success = 'Networking settings saved. DNS/ingress reconciliation will run automatically if mode changed.';
			cfTunnelToken = '';
			cfApiToken = '';
			await refreshSettings();
		} catch (e: any) {
			error = e.message || 'Failed to save settings';
		} finally {
			saving = false;
		}
	}

	async function deployTunnel() {
		saving = true;
		error = '';
		success = '';
		try {
			if (!cfTunnelToken.trim()) {
				error = 'Paste the tunnel token first.';
				saving = false;
				return;
			}
			await api.put('/networking', {
				ingress_mode: 'cloudflare_tunnel',
				tunnel_token: cfTunnelToken,
				cf_api_token: cfApiToken || undefined,
			});
			success = 'Tunnel deployed! Checking status...';
			cfTunnelToken = '';
			cfApiToken = '';
			await refreshSettings();
			tunnelStep = 4;
		} catch (e: any) {
			error = e.message || 'Failed to deploy tunnel';
		} finally {
			saving = false;
		}
	}

	async function testTunnel() {
		try {
			const result = await api.post('/networking/test-tunnel', {});
			tunnelRunning = result.running;
			if (result.running) {
				success = 'Tunnel is running and healthy.';
			} else {
				error = result.message || 'Tunnel is not running.';
			}
		} catch (e: any) {
			error = e.message || 'Failed to test tunnel';
		}
	}
</script>

<svelte:head><title>Networking | Hive</title></svelte:head>

<div class="max-w-4xl mx-auto p-6">
	<h2 class="text-2xl font-bold mb-2">Networking & Ingress</h2>
	<p class="text-sm mb-8" style="color: var(--color-text-muted);">Configure how traffic reaches your apps.</p>

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);">
			<p class="text-sm" style="color: var(--color-danger);">{error}</p>
			<button onclick={() => (error = '')} class="text-xs mt-1 underline" style="color: var(--color-danger);">Dismiss</button>
		</div>
	{/if}

	{#if success}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(34, 197, 94, 0.1); border: 1px solid var(--color-success);">
			<p class="text-sm" style="color: var(--color-success);">{success}</p>
			<button onclick={() => (success = '')} class="text-xs mt-1 underline" style="color: var(--color-success);">Dismiss</button>
		</div>
	{/if}

	<!-- Ingress mode selection -->
	<div class="section-card mb-6">
		<div class="flex items-center justify-between mb-4 gap-3">
			<h3 class="text-lg font-semibold">Ingress Mode</h3>
			<span class="text-xs px-2 py-1 rounded-full" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text-muted);">
				Current: {ingressMode}
			</span>
		</div>
		<div class="grid grid-cols-1 md:grid-cols-3 gap-3">
			{#each [
				{ id: 'port_forward', label: 'Port Forward', desc: 'Forward ports 80/443 from your router to this server' },
				{ id: 'cloudflare_tunnel', label: 'Cloudflare Tunnel', desc: 'Secure tunnel, no port forwarding needed' },
				{ id: 'both', label: 'Both', desc: 'Port forward with tunnel as fallback' },
			] as mode}
				<button
					class="mode-card"
					class:active={ingressMode === mode.id}
					onclick={() => ingressMode = mode.id}
				>
					<p class="font-medium text-sm mb-1">{mode.label}</p>
					<p class="text-xs" style="color: var(--color-text-muted);">{mode.desc}</p>
				</button>
			{/each}
		</div>

		<div class="mt-4 flex items-center justify-between gap-2 flex-wrap">
			<p class="text-xs" style="color: var(--color-text-muted);">
				Changing ingress mode triggers managed DNS target reconciliation.
			</p>
			<button onclick={saveSettings} disabled={saving || !ingressDirty} class="save-btn">
				{saving ? 'Saving...' : ingressDirty ? 'Save Ingress Mode' : 'Saved'}
			</button>
		</div>
	</div>

	<!-- Cloudflare Tunnel guided setup -->
	{#if ingressMode === 'cloudflare_tunnel' || ingressMode === 'both'}
		<div class="section-card mb-6">
			<div class="flex items-center gap-3 mb-5">
				<svg class="w-7 h-7" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="color: #f38020;">
					<path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/>
				</svg>
				<div>
					<h3 class="text-lg font-semibold">Cloudflare Tunnel Setup</h3>
					<p class="text-xs" style="color: var(--color-text-muted);">Expose Hive securely without opening firewall ports</p>
				</div>
			</div>

			<!-- Tunnel status -->
			{#if tunnelRunning}
				<div class="rounded-lg p-4 mb-5 flex items-center justify-between" style="background-color: rgba(34, 197, 94, 0.08); border: 1px solid rgba(34, 197, 94, 0.2);">
					<div class="flex items-center gap-3">
						<span class="w-3 h-3 rounded-full" style="background-color: var(--color-success);"></span>
						<div>
							<p class="text-sm font-medium" style="color: var(--color-success);">Tunnel Running</p>
							{#if tunnelTokenMasked}
								<p class="text-xs font-mono" style="color: var(--color-text-muted);">Token: {tunnelTokenMasked}</p>
							{/if}
							{#if tunnelCNAME}
								<p class="text-xs font-mono" style="color: var(--color-text-muted);">CNAME Target: {tunnelCNAME}</p>
							{/if}
						</div>
					</div>
					<button onclick={testTunnel} class="px-3 py-1.5 rounded text-xs font-medium" style="border: 1px solid var(--color-border);">
						Test
					</button>
				</div>
			{/if}

			<!-- Steps -->
			<div class="steps">
				<!-- Step 1 -->
				<div class="step" class:step-active={tunnelStep === 1} class:step-done={tunnelStep > 1}>
					<div class="step-number">{tunnelStep > 1 ? '&#10003;' : '1'}</div>
					<div class="flex-1">
						<p class="text-sm font-medium">Create a tunnel in Cloudflare</p>
						<p class="text-xs mt-0.5" style="color: var(--color-text-muted);">Go to your Cloudflare dashboard and create a new tunnel.</p>
						{#if tunnelStep === 1}
							<div class="mt-3 flex gap-2 items-center">
								<a
									href="https://one.dash.cloudflare.com/networks/tunnels/create"
									target="_blank"
									class="px-3 py-1.5 rounded text-xs font-medium text-white"
									style="background-color: #f38020;"
								>
									Open Cloudflare Dashboard
								</a>
								<button onclick={() => tunnelStep = 2} class="px-3 py-1.5 rounded text-xs font-medium" style="border: 1px solid var(--color-border);">
									I've created it &rarr;
								</button>
							</div>
						{/if}
					</div>
				</div>

				<!-- Step 2 -->
				<div class="step" class:step-active={tunnelStep === 2} class:step-done={tunnelStep > 2}>
					<div class="step-number">{tunnelStep > 2 ? '&#10003;' : '2'}</div>
					<div class="flex-1">
						<p class="text-sm font-medium">Copy the tunnel token</p>
						<p class="text-xs mt-0.5" style="color: var(--color-text-muted);">Copy the connector install command token from the tunnel setup page.</p>
						{#if tunnelStep === 2}
							<div class="mt-3 space-y-3">
								<input
									type="password"
									bind:value={cfTunnelToken}
									placeholder="eyJh... (paste tunnel token)"
									class="w-full rounded px-3 py-2 text-sm font-mono"
									style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
								/>
								<div>
									<label for="cf-api-token" class="block text-xs font-medium mb-1" style="color: var(--color-text-muted);">CF API Token (optional, for DNS-01 wildcard certs)</label>
									<input
										id="cf-api-token"
										type="password"
										bind:value={cfApiToken}
										placeholder="CF API token"
										class="w-full rounded px-3 py-2 text-sm"
										style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
									/>
									{#if cfApiTokenMasked}
										<p class="text-xs mt-1" style="color: var(--color-text-muted);">Current: {cfApiTokenMasked}</p>
									{/if}
								</div>
								<div class="flex gap-2">
									<button onclick={() => tunnelStep = 1} class="px-3 py-1.5 rounded text-xs font-medium" style="border: 1px solid var(--color-border);">
										&larr; Back
									</button>
									<button onclick={() => { if (cfTunnelToken.trim()) tunnelStep = 3; else error = 'Paste the tunnel token first.'; }} class="px-3 py-1.5 rounded text-xs font-medium" style="border: 1px solid var(--color-border);">
										Next &rarr;
									</button>
								</div>
							</div>
						{/if}
					</div>
				</div>

				<!-- Step 3 -->
				<div class="step" class:step-active={tunnelStep === 3} class:step-done={tunnelStep > 3}>
					<div class="step-number">{tunnelStep > 3 ? '&#10003;' : '3'}</div>
					<div class="flex-1">
						<p class="text-sm font-medium">Deploy the tunnel</p>
						<p class="text-xs mt-0.5" style="color: var(--color-text-muted);">Hive will deploy a <code>cloudflared</code> service in Docker Swarm.</p>
						{#if tunnelStep === 3}
							<div class="mt-3 flex gap-2">
								<button onclick={() => tunnelStep = 2} class="px-3 py-1.5 rounded text-xs font-medium" style="border: 1px solid var(--color-border);">
									&larr; Back
								</button>
								<button
									onclick={deployTunnel}
									disabled={saving}
									class="px-4 py-1.5 rounded text-xs font-medium text-white"
									style="background-color: #f38020; opacity: {saving ? 0.6 : 1};"
								>
									{saving ? 'Deploying...' : 'Deploy Tunnel'}
								</button>
							</div>
						{/if}
					</div>
				</div>

				<!-- Step 4 -->
				<div class="step" class:step-active={tunnelStep === 4} class:step-done={tunnelRunning}>
					<div class="step-number">{tunnelRunning ? '&#10003;' : '4'}</div>
					<div class="flex-1">
						<p class="text-sm font-medium">Configure public hostname</p>
						<p class="text-xs mt-0.5" style="color: var(--color-text-muted);">
							In Cloudflare, add a public hostname for your tunnel. Point <code>*.<em>your-domain.com</em></code> to <code>http://hive-traefik:80</code>.
						</p>
						{#if tunnelStep === 4}
							<div class="mt-3">
								<a
									href="https://one.dash.cloudflare.com/networks/tunnels"
									target="_blank"
									class="px-3 py-1.5 rounded text-xs font-medium inline-flex items-center gap-1"
									style="border: 1px solid #f38020; color: #f38020;"
								>
									Configure in Cloudflare
									<svg class="w-3 h-3" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M5.22 14.78a.75.75 0 001.06 0l7.22-7.22v5.69a.75.75 0 001.5 0v-7.5a.75.75 0 00-.75-.75h-7.5a.75.75 0 000 1.5h5.69l-7.22 7.22a.75.75 0 000 1.06z"/></svg>
								</a>
							</div>
						{/if}
					</div>
				</div>
			</div>
		</div>
	{/if}

	<!-- Connectivity check -->
	<div class="section-card">
		<div class="flex items-center justify-between mb-4">
			<h3 class="text-lg font-semibold">Connectivity Check</h3>
			<button onclick={checkConnectivity} class="px-4 py-2 rounded text-sm font-medium" style="background-color: var(--color-primary); color: var(--color-bg);">
				Check Now
			</button>
		</div>
		{#if connectivity}
			<div class="grid grid-cols-2 gap-4">
				<div class="p-3 rounded-lg" style="background-color: var(--color-bg);">
					<p class="text-sm font-medium">Port 80</p>
					<p class="text-sm" style="color: {connectivity.port_80 ? 'var(--color-success)' : 'var(--color-danger)'};">
						{connectivity.port_80 ? 'Accessible' : 'Not accessible'}
					</p>
				</div>
				<div class="p-3 rounded-lg" style="background-color: var(--color-bg);">
					<p class="text-sm font-medium">Port 443</p>
					<p class="text-sm" style="color: {connectivity.port_443 ? 'var(--color-success)' : 'var(--color-danger)'};">
						{connectivity.port_443 ? 'Accessible' : 'Not accessible'}
					</p>
				</div>
			</div>
			<p class="text-sm mt-3" style="color: var(--color-text-muted);">{connectivity.message}</p>
		{/if}
	</div>
</div>

<style>
	.section-card {
		padding: 1.5rem;
		border-radius: var(--radius-lg);
		background-color: var(--color-surface);
		border: 1px solid var(--color-border);
	}

	.mode-card {
		padding: 1rem;
		border-radius: var(--radius-md);
		text-align: left;
		background-color: var(--color-surface);
		border: 2px solid var(--color-border);
		cursor: pointer;
		transition: border-color 0.15s;
	}
	.mode-card.active {
		border-color: var(--color-primary);
	}

	.save-btn {
		padding: 0.5rem 1.5rem;
		border-radius: var(--radius-md);
		font-size: 0.875rem;
		font-weight: 500;
		background-color: var(--color-primary);
		color: var(--color-bg);
		border: none;
		cursor: pointer;
	}
	.save-btn:disabled { opacity: 0.5; }

	.steps {
		display: flex;
		flex-direction: column;
		gap: 0;
	}
	.step {
		display: flex;
		gap: 0.75rem;
		padding: 1rem 0;
		border-top: 1px solid var(--color-border);
		opacity: 0.5;
		transition: opacity 0.2s;
	}
	.step:first-child { border-top: none; }
	.step-active, .step-done { opacity: 1; }

	.step-number {
		width: 24px;
		height: 24px;
		border-radius: 50%;
		display: flex;
		align-items: center;
		justify-content: center;
		font-size: 0.75rem;
		font-weight: 600;
		flex-shrink: 0;
		background-color: var(--color-bg);
		border: 1px solid var(--color-border);
	}
	.step-done .step-number {
		background-color: var(--color-success);
		border-color: var(--color-success);
		color: white;
	}
	.step-active .step-number {
		background-color: var(--color-primary);
		border-color: var(--color-primary);
		color: white;
	}
</style>
