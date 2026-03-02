<script lang="ts">
	import { api, type StorageHost } from '$lib/api';
	import { onMount } from 'svelte';

	let hosts: StorageHost[] = [];
	let loading = true;
	let showForm = false;
	let testResults: Record<string, { ok: boolean; message: string }> = {};

	let form = {
		name: '',
		node_id: '',
		address: '',
		type: 'nas',
		default_export_path: '',
		default_mount_type: 'nfs',
		mount_options_default: '',
		credentials: '',
		capabilities: {} as Record<string, boolean>,
	};

	const hostTypes = ['nas', 'ceph', 'local-only'];
	const mountTypes = ['nfs', 'cifs', 'cephfs', 'rbd'];

	onMount(loadHosts);

	async function loadHosts() {
		loading = true;
		try {
			hosts = await api.listStorageHosts();
		} catch (e) {
			console.error('Failed to load storage hosts', e);
		}
		loading = false;
	}

	async function createHost() {
		try {
			await api.createStorageHost({
				name: form.name,
				node_id: form.node_id || undefined,
				address: form.address,
				type: form.type,
				default_export_path: form.default_export_path,
				default_mount_type: form.default_mount_type,
				mount_options_default: form.mount_options_default,
				credentials: form.credentials || undefined,
				capabilities: Object.keys(form.capabilities).length > 0 ? form.capabilities : undefined,
			});
			resetForm();
			await loadHosts();
		} catch (e: any) {
			alert(e.message);
		}
	}

	async function deleteHost(id: string) {
		if (!confirm('Delete this storage host? Volumes referencing it will lose their storage host association.')) return;
		try {
			await api.deleteStorageHost(id);
			await loadHosts();
		} catch (e: any) {
			alert(e.message);
		}
	}

	async function testConnectivity(id: string) {
		try {
			const result = await api.testStorageHostConnectivity(id);
			testResults[id] = { ok: result.ok, message: result.message };
			testResults = testResults;
		} catch (e: any) {
			testResults[id] = { ok: false, message: e.message };
			testResults = testResults;
		}
	}

	function resetForm() {
		form = {
			name: '', node_id: '', address: '', type: 'nas',
			default_export_path: '', default_mount_type: 'nfs',
			mount_options_default: '', credentials: '',
			capabilities: {},
		};
		showForm = false;
	}

	function toggleCapability(cap: string) {
		form.capabilities = { ...form.capabilities, [cap]: !form.capabilities[cap] };
	}

	function formatDate(d: string) {
		return new Date(d).toLocaleDateString();
	}

	function statusColor(status: string) {
		if (status === 'active') return 'color: #22c55e;';
		if (status === 'degraded') return 'color: #f59e0b;';
		return 'color: #ef4444;';
	}
</script>

<div class="max-w-5xl mx-auto">
	<div class="flex items-center justify-between mb-6">
		<div>
			<h2 class="text-2xl font-bold">Storage Hosts</h2>
			<p class="text-sm mt-1" style="color: var(--color-text-muted);">Manage NAS, Ceph, and local storage nodes for intelligent volume mounting.</p>
		</div>
		<button class="px-4 py-2 rounded-lg text-sm font-medium"
			style="background-color: var(--color-primary); color: white;"
			on:click={() => showForm = !showForm}>
			{showForm ? 'Cancel' : '+ Add Storage Host'}
		</button>
	</div>

	{#if showForm}
		<form on:submit|preventDefault={createHost}
			class="rounded-lg p-6 mb-6 space-y-4"
			style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="font-semibold text-lg">Register Storage Host</h3>

			<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
				<div>
					<label class="block text-sm font-medium mb-1">Name</label>
					<input bind:value={form.name} required placeholder="truenas-main"
						class="w-full px-3 py-2 rounded text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
				<div>
					<label class="block text-sm font-medium mb-1">Address (IP or hostname)</label>
					<input bind:value={form.address} required placeholder="10.0.0.50"
						class="w-full px-3 py-2 rounded text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
				<div>
					<label class="block text-sm font-medium mb-1">Type</label>
					<select bind:value={form.type}
						class="w-full px-3 py-2 rounded text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border);">
						{#each hostTypes as t}
							<option value={t}>{t}</option>
						{/each}
					</select>
				</div>
				<div>
					<label class="block text-sm font-medium mb-1">Default Mount Type</label>
					<select bind:value={form.default_mount_type}
						class="w-full px-3 py-2 rounded text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border);">
						{#each mountTypes as mt}
							<option value={mt}>{mt}</option>
						{/each}
					</select>
				</div>
				<div>
					<label class="block text-sm font-medium mb-1">Docker Node ID (optional, for swarm member NAS)</label>
					<input bind:value={form.node_id} placeholder="Node ID from Swarm"
						class="w-full px-3 py-2 rounded text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
				<div>
					<label class="block text-sm font-medium mb-1">Default Export Path</label>
					<input bind:value={form.default_export_path} placeholder="/mnt/pool/docker"
						class="w-full px-3 py-2 rounded text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
				<div class="md:col-span-2">
					<label class="block text-sm font-medium mb-1">Mount Options</label>
					<input bind:value={form.mount_options_default} placeholder="soft,nolock,rsize=65536"
						class="w-full px-3 py-2 rounded text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
				<div class="md:col-span-2">
					<label class="block text-sm font-medium mb-1">Credentials (CIFS password / Ceph keyring, encrypted at rest)</label>
					<input bind:value={form.credentials} type="password" placeholder="Optional"
						class="w-full px-3 py-2 rounded text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
			</div>

			<div>
				<label class="block text-sm font-medium mb-2">Capabilities</label>
				<div class="flex gap-4 flex-wrap">
					{#each ['nfs', 'cifs', 'cephfs', 'rbd', 'smb_multichannel'] as cap}
						<label class="flex items-center gap-1.5 text-sm">
							<input type="checkbox" checked={form.capabilities[cap]}
								on:change={() => toggleCapability(cap)} />
							{cap}
						</label>
					{/each}
				</div>
			</div>

			<div class="flex justify-end">
				<button type="submit" class="px-4 py-2 rounded-lg text-sm font-medium"
					style="background-color: var(--color-primary); color: white;">
					Register Host
				</button>
			</div>
		</form>
	{/if}

	{#if loading}
		<p style="color: var(--color-text-muted);">Loading...</p>
	{:else if hosts.length === 0}
		<div class="rounded-lg p-8 text-center" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<p class="text-lg font-medium mb-2">No storage hosts registered</p>
			<p class="text-sm" style="color: var(--color-text-muted);">Add a NAS, Ceph cluster, or local storage node to enable smart volume resolution.</p>
		</div>
	{:else}
		<div class="space-y-4">
			{#each hosts as host}
				<div class="rounded-lg p-5" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
					<div class="flex items-start justify-between">
						<div>
							<div class="flex items-center gap-3 mb-1">
								<h3 class="font-semibold text-lg">{host.name}</h3>
								<span class="text-xs px-2 py-0.5 rounded-full font-medium"
									style="background-color: var(--color-bg); {statusColor(host.status)}">
									{host.status}
								</span>
								<span class="text-xs px-2 py-0.5 rounded-full"
									style="background-color: var(--color-bg); color: var(--color-text-muted);">
									{host.type}
								</span>
							</div>
							<div class="text-sm space-y-1" style="color: var(--color-text-muted);">
								<p>Address: <span style="color: var(--color-text);">{host.address}</span></p>
								{#if host.node_id}
									<p>Swarm Node: <span class="font-mono text-xs" style="color: var(--color-text);">{host.node_id.slice(0, 12)}</span></p>
								{/if}
								<p>Mount: <span style="color: var(--color-text);">{host.default_mount_type}</span>
									{#if host.default_export_path}
										at <span class="font-mono text-xs" style="color: var(--color-text);">{host.default_export_path}</span>
									{/if}
								</p>
								{#if host.node_label}
									<p>Label: <span class="font-mono text-xs" style="color: var(--color-text);">{host.node_label}</span></p>
								{/if}
								<p class="text-xs">Registered {formatDate(host.created_at)}</p>
							</div>
							{#if host.capabilities && Object.keys(host.capabilities).length > 0}
								<div class="flex gap-2 mt-2 flex-wrap">
									{#each Object.entries(host.capabilities).filter(([,v]) => v) as [cap]}
										<span class="text-xs px-2 py-0.5 rounded-full"
											style="background-color: var(--color-bg); color: var(--color-text-muted);">
											{cap}
										</span>
									{/each}
								</div>
							{/if}
						</div>
						<div class="flex gap-2">
							<button class="px-3 py-1.5 rounded text-sm"
								style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
								on:click={() => testConnectivity(host.id)}>
								Test
							</button>
							<button class="px-3 py-1.5 rounded text-sm"
								style="background-color: #ef4444; color: white;"
								on:click={() => deleteHost(host.id)}>
								Delete
							</button>
						</div>
					</div>
					{#if testResults[host.id]}
						<div class="mt-3 px-3 py-2 rounded text-sm"
							style="background-color: var(--color-bg); border: 1px solid var(--color-border); {testResults[host.id].ok ? 'color: #22c55e;' : 'color: #ef4444;'}">
							{testResults[host.id].message}
						</div>
					{/if}
				</div>
			{/each}
		</div>
	{/if}
</div>
