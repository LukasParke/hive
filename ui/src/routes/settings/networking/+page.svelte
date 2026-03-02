<script lang="ts">
	import { api, type ConnectivityResult } from '$lib/api';
	import { onMount } from 'svelte';

	let ingressMode = $state('port_forward');
	let cfApiToken = $state('');
	let cfTunnelToken = $state('');
	let connectivity = $state<ConnectivityResult | null>(null);
	let saving = $state(false);
	let error = $state('');
	let success = $state('');

	onMount(async () => {
		try {
			connectivity = await api.checkConnectivity();
		} catch {}
	});

	async function checkConnectivity() {
		try {
			connectivity = await api.checkConnectivity();
		} catch (e: any) {
			error = e.message;
		}
	}
</script>

<div class="max-w-4xl mx-auto">
	<h2 class="text-2xl font-bold mb-6">Networking & Ingress</h2>

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid #ef4444;">
			<p style="color: #ef4444;">{error}</p>
		</div>
	{/if}

	{#if success}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(34, 197, 94, 0.1); border: 1px solid #22c55e;">
			<p style="color: #22c55e;">{success}</p>
		</div>
	{/if}

	<div class="rounded-lg p-6 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
		<h3 class="text-lg font-semibold mb-4">Ingress Mode</h3>
		<div class="grid grid-cols-1 md:grid-cols-3 gap-4 mb-4">
			<button class="p-4 rounded-lg text-left" style="border: 2px solid {ingressMode === 'port_forward' ? '#3b82f6' : 'var(--color-border)'}; background-color: var(--color-surface);" onclick={() => ingressMode = 'port_forward'}>
				<p class="font-medium mb-1">Port Forward</p>
				<p class="text-xs" style="color: var(--color-text-muted);">Forward ports 80/443 from your router</p>
			</button>
			<button class="p-4 rounded-lg text-left" style="border: 2px solid {ingressMode === 'cloudflare_tunnel' ? '#3b82f6' : 'var(--color-border)'}; background-color: var(--color-surface);" onclick={() => ingressMode = 'cloudflare_tunnel'}>
				<p class="font-medium mb-1">Cloudflare Tunnel</p>
				<p class="text-xs" style="color: var(--color-text-muted);">No port forwarding needed</p>
			</button>
			<button class="p-4 rounded-lg text-left" style="border: 2px solid {ingressMode === 'both' ? '#3b82f6' : 'var(--color-border)'}; background-color: var(--color-surface);" onclick={() => ingressMode = 'both'}>
				<p class="font-medium mb-1">Both</p>
				<p class="text-xs" style="color: var(--color-text-muted);">Port forward + tunnel fallback</p>
			</button>
		</div>
	</div>

	{#if ingressMode === 'cloudflare_tunnel' || ingressMode === 'both'}
		<div class="rounded-lg p-6 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="text-lg font-semibold mb-4">Cloudflare Configuration</h3>
			<div class="space-y-4">
				<div>
					<label class="block text-sm font-medium mb-1">API Token</label>
					<input type="password" bind:value={cfApiToken} placeholder="CF API Token for DNS-01 challenges"
						class="w-full px-3 py-2 rounded-lg text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
				<div>
					<label class="block text-sm font-medium mb-1">Tunnel Token</label>
					<input type="password" bind:value={cfTunnelToken} placeholder="Cloudflare tunnel token"
						class="w-full px-3 py-2 rounded-lg text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
			</div>
		</div>
	{/if}

	<div class="rounded-lg p-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
		<div class="flex items-center justify-between mb-4">
			<h3 class="text-lg font-semibold">Connectivity Check</h3>
			<button onclick={checkConnectivity} class="px-4 py-2 rounded-lg text-sm font-medium" style="background-color: #3b82f6; color: white;">
				Check Now
			</button>
		</div>
		{#if connectivity}
			<div class="grid grid-cols-2 gap-4">
				<div class="p-3 rounded-lg" style="background-color: var(--color-bg);">
					<p class="text-sm font-medium">Port 80</p>
					<p class="text-sm" style="color: {connectivity.port_80 ? '#22c55e' : '#ef4444'};">
						{connectivity.port_80 ? 'Accessible' : 'Not accessible'}
					</p>
				</div>
				<div class="p-3 rounded-lg" style="background-color: var(--color-bg);">
					<p class="text-sm font-medium">Port 443</p>
					<p class="text-sm" style="color: {connectivity.port_443 ? '#22c55e' : '#ef4444'};">
						{connectivity.port_443 ? 'Accessible' : 'Not accessible'}
					</p>
				</div>
			</div>
			<p class="text-sm mt-3" style="color: var(--color-text-muted);">{connectivity.message}</p>
		{/if}
	</div>
</div>
