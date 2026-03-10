<script lang="ts">
	import { api } from '$lib/api';
	import { invalidateAll } from '$app/navigation';
	import type { VPNServer, VPNPeer } from '$lib/types';
	import { Button, Badge, EmptyState, Alert } from '$lib/components';

	let { data } = $props();

	let servers = $derived(data.servers ?? []);
	let serverPeers = $state<Record<string, VPNPeer[]>>({});
	let expandedServer = $state<string | null>(null);
	let error = $state('');
	let loading = $state<string | null>(null);
	let showForm = $state(false);
	let formName = $state('');
	let formEndpoint = $state('');
	let formListenPort = $state('51820');
	let addPeerServerId = $state<string | null>(null);
	let addPeerName = $state('');

	async function loadPeers(serverId: string) {
		if (serverPeers[serverId]) return;
		try {
			const peers = await api.listVPNPeers(serverId);
			serverPeers = { ...serverPeers, [serverId]: peers };
		} catch {
			serverPeers = { ...serverPeers, [serverId]: [] };
		}
	}

	function toggleExpand(serverId: string) {
		if (expandedServer === serverId) {
			expandedServer = null;
		} else {
			expandedServer = serverId;
			loadPeers(serverId);
		}
	}

	async function createServer() {
		if (!formName.trim()) return;
		loading = 'create';
		error = '';
		try {
			await api.createVPNServer({
				name: formName.trim(),
				endpoint: formEndpoint.trim() || undefined,
				listen_port: formListenPort ? parseInt(formListenPort, 10) : undefined
			});
			await invalidateAll();
			showForm = false;
			formName = '';
			formEndpoint = '';
			formListenPort = '51820';
		} catch (e: unknown) {
			error = e instanceof Error ? e.message : 'Failed to create VPN server';
		} finally {
			loading = null;
		}
	}

	async function deleteServer(id: string) {
		loading = id;
		error = '';
		try {
			await api.deleteVPNServer(id);
			await invalidateAll();
			expandedServer = expandedServer === id ? null : expandedServer;
			const { [id]: _, ...rest } = serverPeers;
			serverPeers = rest;
		} catch (e: unknown) {
			error = e instanceof Error ? e.message : 'Failed to delete VPN server';
		} finally {
			loading = null;
		}
	}

	async function addPeer(serverId: string) {
		if (!addPeerName.trim()) return;
		loading = `add-peer-${serverId}`;
		error = '';
		try {
			const peer = await api.createVPNPeer(serverId, { name: addPeerName.trim() });
			serverPeers = {
				...serverPeers,
				[serverId]: [...(serverPeers[serverId] ?? []), peer]
			};
			addPeerServerId = null;
			addPeerName = '';
		} catch (e: unknown) {
			error = e instanceof Error ? e.message : 'Failed to add peer';
		} finally {
			loading = null;
		}
	}

	async function deletePeer(serverId: string, peerId: string) {
		loading = `del-peer-${peerId}`;
		error = '';
		try {
			await api.deleteVPNPeer(serverId, peerId);
			serverPeers = {
				...serverPeers,
				[serverId]: (serverPeers[serverId] ?? []).filter((p) => p.id !== peerId)
			};
			await invalidateAll();
		} catch (e: unknown) {
			error = e instanceof Error ? e.message : 'Failed to delete peer';
		} finally {
			loading = null;
		}
	}

	function serverStatus(server: VPNServer): string {
		return server.enabled !== false ? 'active' : 'disabled';
	}
</script>

<svelte:head><title>VPN | Hive</title></svelte:head>

<div class="max-w-4xl mx-auto">
	<div class="page-header">
		<div>
			<h2 class="page-title">VPN</h2>
			<p class="page-subtitle">WireGuard VPN servers and peers</p>
		</div>
		<Button variant="primary" onclick={() => (showForm = !showForm)}>
			{showForm ? 'Cancel' : 'Create Server'}
		</Button>
	</div>

	{#if error}
		<Alert variant="danger" class="mb-4">
			<p style="color: var(--color-danger);">{error}</p>
		</Alert>
	{/if}

	{#if showForm}
		<div class="hive-card mb-6">
			<div class="hive-card-body space-y-4">
				<h3 class="text-sm font-semibold text-slate-300">Create VPN server</h3>
				<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
					<div>
						<label for="vpn-form-name" class="block text-xs font-medium text-slate-400 mb-1">Name</label>
						<input
							id="vpn-form-name"
							type="text"
							bind:value={formName}
							placeholder="e.g. homelab-vpn"
							class="w-full px-3 py-2 rounded-lg bg-slate-900 border border-slate-700 text-slate-200 placeholder-slate-500 focus:border-amber-500/50 focus:ring-1 focus:ring-amber-500/30"
						/>
					</div>
					<div>
						<label for="vpn-form-endpoint" class="block text-xs font-medium text-slate-400 mb-1">Endpoint (host:port)</label>
						<input
							id="vpn-form-endpoint"
							type="text"
							bind:value={formEndpoint}
							placeholder="vpn.example.com:51820"
							class="w-full px-3 py-2 rounded-lg bg-slate-900 border border-slate-700 text-slate-200 placeholder-slate-500 focus:border-amber-500/50 focus:ring-1 focus:ring-amber-500/30"
						/>
					</div>
					<div>
						<label for="vpn-form-port" class="block text-xs font-medium text-slate-400 mb-1">Listen port</label>
						<input
							id="vpn-form-port"
							type="number"
							bind:value={formListenPort}
							placeholder="51820"
							class="w-full px-3 py-2 rounded-lg bg-slate-900 border border-slate-700 text-slate-200 placeholder-slate-500 focus:border-amber-500/50 focus:ring-1 focus:ring-amber-500/30"
						/>
					</div>
				</div>
				<div class="flex gap-2">
					<Button variant="primary" onclick={createServer} disabled={!formName.trim()} loading={loading === 'create'}>
						Create
					</Button>
					<Button variant="ghost" onclick={() => { showForm = false; formName = ''; formEndpoint = ''; }}>Cancel</Button>
				</div>
			</div>
		</div>
	{/if}

	<div class="space-y-3">
		{#each servers as server (server.id)}
			<div class="hive-card overflow-hidden">
				<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions a11y_no_noninteractive_element_interactions -->
				<div
					role="button"
					tabindex="0"
					class="hive-card-body flex flex-col sm:flex-row sm:items-center justify-between gap-4 cursor-pointer"
					onclick={() => toggleExpand(server.id)}
					onkeydown={(e) => e.key === 'Enter' && toggleExpand(server.id)}
				>
					<div class="flex-1 min-w-0">
						<div class="flex flex-wrap items-center gap-2 mb-1">
							<h3 class="font-semibold text-slate-200">{server.name}</h3>
							<Badge variant={serverStatus(server) === 'active' ? 'success' : 'neutral'} dot>
								{serverStatus(server)}
							</Badge>
						</div>
						<div class="flex flex-wrap gap-3 text-sm text-slate-400">
							{#if server.endpoint}
								<span>Endpoint: {server.endpoint}</span>
							{/if}
							<span>Port: {server.listen_port ?? 51820}</span>
							<span>Peers: {server.peer_count ?? (serverPeers[server.id]?.length ?? 0)}</span>
						</div>
					</div>
					<div
						class="flex items-center gap-2"
						role="presentation"
						onclick={(e) => e.stopPropagation()}
					>
						<svg
							class="w-5 h-5 text-slate-400 transition-transform {expandedServer === server.id ? 'rotate-180' : ''}"
							fill="none"
							stroke="currentColor"
							viewBox="0 0 24 24"
						>
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
						</svg>
						<Button
							variant="danger"
							size="sm"
							onclick={() => deleteServer(server.id)}
							disabled={loading === server.id}
							loading={loading === server.id}
						>
							Delete
						</Button>
					</div>
				</div>

				{#if expandedServer === server.id}
					<div class="border-t border-slate-700/80 p-4 space-y-4 bg-slate-900/30">
						<div class="flex items-center justify-between">
							<h4 class="text-sm font-medium text-slate-300">Peers</h4>
							{#if addPeerServerId === server.id}
								<div class="flex gap-2 items-center">
									<input
										type="text"
										bind:value={addPeerName}
										placeholder="Peer name"
										class="px-2 py-1.5 rounded text-sm bg-slate-800 border border-slate-600 text-slate-200 w-40"
										onkeydown={(e) => e.key === 'Enter' && addPeer(server.id)}
									/>
									<Button size="sm" variant="primary" onclick={() => addPeer(server.id)} disabled={!addPeerName.trim()} loading={loading === `add-peer-${server.id}`}>
										Add
									</Button>
									<Button size="sm" variant="ghost" onclick={() => { addPeerServerId = null; addPeerName = ''; }}>Cancel</Button>
								</div>
							{:else}
								<Button size="sm" variant="secondary" onclick={() => { addPeerServerId = server.id; addPeerName = ''; }}>
									Add Peer
								</Button>
							{/if}
						</div>
						{#if serverPeers[server.id]?.length}
							<ul class="space-y-2">
								{#each serverPeers[server.id] as peer (peer.id)}
									<li class="flex items-center justify-between py-2 px-3 rounded-lg bg-slate-800/50 border border-slate-700/50">
										<div>
											<span class="font-medium text-slate-300">{peer.name}</span>
											<span class="text-xs text-slate-500 ml-2">IP: {peer.assigned_ip || '—'}</span>
										</div>
										<Button
											size="sm"
											variant="ghost"
											class="text-red-400 hover:text-red-300"
											onclick={() => deletePeer(server.id, peer.id)}
											disabled={loading === `del-peer-${peer.id}`}
											loading={loading === `del-peer-${peer.id}`}
										>
											Remove
										</Button>
									</li>
								{/each}
							</ul>
						{:else}
							<p class="text-sm text-slate-500">No peers yet. Add a peer to generate a WireGuard config.</p>
						{/if}
					</div>
				{/if}
			</div>
		{/each}
	</div>

	{#if servers.length === 0 && !showForm}
		<EmptyState
			title="No VPN servers"
			description="Create a WireGuard VPN server to provide secure access to your homelab."
		/>
	{/if}
</div>
