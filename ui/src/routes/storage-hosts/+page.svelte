<script lang="ts">
	import { api } from '$lib/api';
	import { invalidateAll } from '$app/navigation';
	import type { StorageHost } from '$lib/types';

	let { data } = $props();

	let showForm = $state(false);
	let editingId = $state<string | null>(null);
	let testResults = $state<Record<string, { ok: boolean; message: string }>>({});
	let testing = $state<Record<string, boolean>>({});
	let saving = $state(false);

	let form = $state(defaultForm());

	const hostTypes = ['nas', 'ceph', 'local-only'];
	const mountTypes = ['nfs', 'cifs', 'cephfs', 'rbd'];

	function defaultForm() {
		return {
			name: '',
			node_id: '',
			address: '',
			type: 'nas',
			default_export_path: '',
			default_mount_type: 'nfs',
			mount_options_default: '',
			credentials: '',
			capabilities: { nfs: true } as Record<string, boolean>,
			status: 'active',
		};
	}

	function testPort(type: string): string {
		switch (type) {
			case 'cifs': case 'smb': return '445';
			case 'ceph': case 'cephfs': return '6789';
			default: return '2049';
		}
	}

	async function submitForm() {
		saving = true;
		try {
			if (editingId) {
				await api.updateStorageHost(editingId, {
					name: form.name,
					node_id: form.node_id || undefined,
					address: form.address,
					type: form.type,
					default_export_path: form.default_export_path,
					default_mount_type: form.default_mount_type,
					mount_options_default: form.mount_options_default,
					credentials: form.credentials || undefined,
					capabilities: Object.keys(form.capabilities).filter(k => form.capabilities[k]).length > 0
						? Object.fromEntries(Object.entries(form.capabilities).filter(([,v]) => v))
						: undefined,
				} as any);
			} else {
				await api.createStorageHost({
					name: form.name,
					node_id: form.node_id || undefined,
					address: form.address,
					type: form.type,
					default_export_path: form.default_export_path,
					default_mount_type: form.default_mount_type,
					mount_options_default: form.mount_options_default,
					credentials: form.credentials || undefined,
					capabilities: Object.keys(form.capabilities).filter(k => form.capabilities[k]).length > 0
						? Object.fromEntries(Object.entries(form.capabilities).filter(([,v]) => v))
						: undefined,
				});
			}
			resetForm();
			await invalidateAll();
		} catch (e: any) {
			alert(e.message);
		} finally {
			saving = false;
		}
	}

	async function deleteHost(id: string) {
		if (!confirm('Delete this storage host? Volumes referencing it will lose their storage host association.')) return;
		try {
			await api.deleteStorageHost(id);
			await invalidateAll();
		} catch (e: any) {
			alert(e.message);
		}
	}

	async function testConnectivity(id: string) {
		testing[id] = true;
		testing = testing;
		try {
			const result = await api.testStorageHostConnectivity(id);
			testResults[id] = { ok: result.ok, message: result.message };
		} catch (e: any) {
			testResults[id] = { ok: false, message: e.message };
		} finally {
			testing[id] = false;
			testing = testing;
			testResults = testResults;
		}
	}

	function startEdit(host: StorageHost) {
		editingId = host.id;
		form = {
			name: host.name,
			node_id: host.node_id || '',
			address: host.address,
			type: host.type,
			default_export_path: host.default_export_path || '',
			default_mount_type: host.default_mount_type || 'nfs',
			mount_options_default: host.mount_options_default || '',
			credentials: '',
			capabilities: host.capabilities && typeof host.capabilities === 'object'
				? { ...host.capabilities }
				: {},
			status: host.status || 'active',
		};
		showForm = true;
	}

	function resetForm() {
		form = defaultForm();
		showForm = false;
		editingId = null;
	}

	function toggleCapability(cap: string) {
		form = { ...form, capabilities: { ...form.capabilities, [cap]: !form.capabilities[cap] } };
	}

	function formatDate(d: string) {
		return new Date(d).toLocaleDateString();
	}

	function statusColor(status: string) {
		if (status === 'active') return 'color: var(--color-success);';
		if (status === 'degraded') return 'color: var(--color-primary);';
		return 'color: var(--color-danger);';
	}

	function typeLabel(type: string) {
		switch (type) {
			case 'nas': return 'NAS';
			case 'ceph': return 'Ceph';
			case 'local-only': return 'Local';
			default: return type;
		}
	}

	function mountTypeLabel(mt: string) {
		return mt.toUpperCase();
	}
</script>

<svelte:head><title>Storage Hosts | Hive</title></svelte:head>

<div class="max-w-5xl mx-auto">
	<div class="flex items-center justify-between mb-6">
		<div>
			<h2 class="text-2xl font-bold">Storage Hosts</h2>
			<p class="text-sm mt-1" style="color: var(--color-text-muted);">Manage NAS, Ceph, and local storage nodes for intelligent volume mounting.</p>
		</div>
		<button class="px-4 py-2 rounded-lg text-sm font-medium"
			style="background-color: var(--color-primary); color: white;"
			onclick={() => { if (showForm && !editingId) { resetForm(); } else { resetForm(); showForm = true; } }}>
			{showForm ? 'Cancel' : '+ Add Storage Host'}
		</button>
	</div>

	{#if showForm}
		<form onsubmit={(e: Event) => { e.preventDefault(); submitForm(); }}
			class="rounded-lg p-6 mb-6 space-y-4"
			style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="font-semibold text-lg">{editingId ? 'Edit Storage Host' : 'Register Storage Host'}</h3>

			<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
				<div>
					<label for="sh-name" class="block text-sm font-medium mb-1">Name</label>
					<input id="sh-name" bind:value={form.name} required placeholder="truenas-main"
						class="w-full px-3 py-2 rounded text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
				<div>
					<label for="sh-address" class="block text-sm font-medium mb-1">Address (IP or hostname)</label>
					<input id="sh-address" bind:value={form.address} required placeholder="10.0.0.50"
						class="w-full px-3 py-2 rounded text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
				<div>
					<label for="sh-type" class="block text-sm font-medium mb-1">Type</label>
					<select id="sh-type" bind:value={form.type}
						class="w-full px-3 py-2 rounded text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border);">
						{#each hostTypes as t}
							<option value={t}>{t === 'nas' ? 'NAS' : t === 'ceph' ? 'Ceph' : 'Local Only'}</option>
						{/each}
					</select>
				</div>
				<div>
					<label for="sh-mount-type" class="block text-sm font-medium mb-1">Default Mount Type</label>
					<select id="sh-mount-type" bind:value={form.default_mount_type}
						class="w-full px-3 py-2 rounded text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border);">
						{#each mountTypes as mt}
							<option value={mt}>{mt.toUpperCase()}</option>
						{/each}
					</select>
				</div>
				<div>
					<label for="sh-node-id" class="block text-sm font-medium mb-1">Swarm Node ID
						<span class="font-normal" style="color: var(--color-text-muted);">(optional)</span>
					</label>
					<input id="sh-node-id" bind:value={form.node_id} placeholder="For swarm-member storage"
						class="w-full px-3 py-2 rounded text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
				<div>
					<label for="sh-export" class="block text-sm font-medium mb-1">Default Export Path</label>
					<input id="sh-export" bind:value={form.default_export_path} placeholder="/mnt/pool/docker"
						class="w-full px-3 py-2 rounded text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
				<div class="md:col-span-2">
					<label for="sh-options" class="block text-sm font-medium mb-1">Mount Options</label>
					<input id="sh-options" bind:value={form.mount_options_default} placeholder="soft,nolock,rsize=65536"
						class="w-full px-3 py-2 rounded text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
				<div class="md:col-span-2">
					<label for="sh-creds" class="block text-sm font-medium mb-1">Credentials
						<span class="font-normal" style="color: var(--color-text-muted);">
							(CIFS password / Ceph keyring, encrypted at rest)
						</span>
					</label>
					<input id="sh-creds" bind:value={form.credentials} type="password"
						placeholder={editingId ? 'Leave blank to keep existing' : 'Optional'}
						class="w-full px-3 py-2 rounded text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
			</div>

			<fieldset>
				<legend class="block text-sm font-medium mb-2">Capabilities</legend>
				<div class="flex gap-4 flex-wrap">
					{#each ['nfs', 'cifs', 'cephfs', 'rbd', 'smb_multichannel'] as cap}
						<label class="flex items-center gap-1.5 text-sm cursor-pointer">
							<input type="checkbox" checked={form.capabilities[cap]}
								onchange={() => toggleCapability(cap)} />
							{cap.toUpperCase().replace('_', ' ')}
						</label>
					{/each}
				</div>
			</fieldset>

			<div class="flex justify-end gap-3">
				<button type="button" class="px-4 py-2 rounded-lg text-sm font-medium"
					style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
					onclick={resetForm}>
					Cancel
				</button>
				<button type="submit" class="px-4 py-2 rounded-lg text-sm font-medium"
					style="background-color: var(--color-primary); color: white; opacity: {saving ? '0.6' : '1'};"
					disabled={saving}>
					{saving ? 'Saving...' : editingId ? 'Save Changes' : 'Register Host'}
				</button>
			</div>
		</form>
	{/if}

	{#if data.hosts.length === 0 && !showForm}
		<div class="rounded-lg p-8 text-center" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<p class="text-lg font-medium mb-2">No storage hosts registered</p>
			<p class="text-sm mb-4" style="color: var(--color-text-muted);">Add a NAS, Ceph cluster, or local storage node to enable smart volume resolution.</p>
			<button class="px-4 py-2 rounded-lg text-sm font-medium"
				style="background-color: var(--color-primary); color: white;"
				onclick={() => { showForm = true; }}>
				+ Add Your First Storage Host
			</button>
		</div>
	{:else}
		<div class="space-y-4">
			{#each data.hosts as host}
				<div class="rounded-lg p-5" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
					<div class="flex items-start justify-between gap-4">
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-3 mb-2 flex-wrap">
								<h3 class="font-semibold text-lg">{host.name}</h3>
								<span class="text-xs px-2 py-0.5 rounded-full font-medium"
									style="background-color: var(--color-bg); {statusColor(host.status || 'unknown')}">
									{host.status || 'unknown'}
								</span>
								<span class="text-xs px-2 py-0.5 rounded-full"
									style="background-color: var(--color-bg); color: var(--color-text-muted);">
									{typeLabel(host.type)}
								</span>
							</div>

							<div class="grid grid-cols-1 sm:grid-cols-2 gap-x-8 gap-y-1 text-sm" style="color: var(--color-text-muted);">
								<p>Address: <span class="font-mono" style="color: var(--color-text);">{host.address}</span></p>
								<p>Mount Type: <span style="color: var(--color-text);">{mountTypeLabel(host.default_mount_type)}</span></p>
								{#if host.default_export_path}
									<p>Export Path: <span class="font-mono text-xs" style="color: var(--color-text);">{host.default_export_path}</span></p>
								{/if}
								{#if host.node_id}
									<p>Swarm Node: <span class="font-mono text-xs" style="color: var(--color-text);">{host.node_id.slice(0, 12)}</span></p>
								{/if}
								{#if host.mount_options_default}
									<p class="sm:col-span-2">Options: <span class="font-mono text-xs" style="color: var(--color-text);">{host.mount_options_default}</span></p>
								{/if}
								{#if host.node_label}
									<p class="sm:col-span-2">Label: <span class="font-mono text-xs" style="color: var(--color-text);">{host.node_label}</span></p>
								{/if}
							</div>

							{#if host.capabilities && Object.entries(host.capabilities).some(([,v]) => v)}
								<div class="flex gap-2 mt-3 flex-wrap">
									{#each Object.entries(host.capabilities).filter(([,v]) => v) as [cap]}
										<span class="text-xs px-2 py-0.5 rounded-full"
											style="background-color: var(--color-bg); color: var(--color-text-muted);">
											{cap.toUpperCase().replace('_', ' ')}
										</span>
									{/each}
								</div>
							{/if}

							<p class="text-xs mt-2" style="color: var(--color-text-muted);">
								Registered {formatDate(host.created_at)}
								{#if host.updated_at && host.updated_at !== host.created_at}
									&middot; Updated {formatDate(host.updated_at)}
								{/if}
							</p>
						</div>

						<div class="flex gap-2 shrink-0">
							<button class="px-3 py-1.5 rounded text-sm"
								style="background-color: var(--color-bg); border: 1px solid var(--color-border);
								       opacity: {testing[host.id] ? '0.6' : '1'};"
								disabled={testing[host.id]}
								onclick={() => testConnectivity(host.id)}>
								{testing[host.id] ? 'Testing...' : 'Test'}
							</button>
							<button class="px-3 py-1.5 rounded text-sm"
								style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
								onclick={() => startEdit(host)}>
								Edit
							</button>
							<button class="px-3 py-1.5 rounded text-sm"
								style="background-color: var(--color-danger); color: white;"
								onclick={() => deleteHost(host.id)}>
								Delete
							</button>
						</div>
					</div>

					{#if testResults[host.id]}
						<div class="mt-3 px-3 py-2 rounded text-sm flex items-center gap-2"
							style="background-color: var(--color-bg); border: 1px solid var(--color-border);
							       {testResults[host.id].ok ? 'color: var(--color-success);' : 'color: var(--color-danger);'}">
							<span>{testResults[host.id].ok ? 'Pass' : 'Fail'}</span>
							<span style="color: var(--color-text-muted);">&middot;</span>
							<span>{testResults[host.id].message}</span>
							<span style="color: var(--color-text-muted);" class="text-xs ml-auto">
								Port {testPort(host.type)}
							</span>
						</div>
					{/if}
				</div>
			{/each}
		</div>
	{/if}
</div>
