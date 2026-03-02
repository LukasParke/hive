<script lang="ts">
	import { page } from '$app/stores';
	import { api, type Volume, type App, type CreateVolumeRequest } from '$lib/api';
	import { onMount } from 'svelte';

	let volumes = $state<Volume[]>([]);
	let apps = $state<App[]>([]);
	let error = $state('');
	let showCreate = $state(false);
	let projectId = $derived($page.params.id ?? '');

	let mountType = $state<'volume' | 'nfs' | 'cifs'>('volume');
	let newVolume = $state<CreateVolumeRequest>({
		name: '',
		mount_type: 'volume',
		remote_host: '',
		remote_path: '',
		mount_options: '',
		username: '',
		password: '',
	});

	let attachingVolumeId = $state<string | null>(null);
	let attachAppId = $state('');
	let attachPath = $state('');
	let attachReadOnly = $state(false);

	onMount(() => loadData());

	async function loadData() {
		try {
			[volumes, apps] = await Promise.all([
				api.listVolumes(projectId),
				api.listApps(projectId),
			]);
		} catch (e: any) {
			error = e.message;
		}
	}

	function onMountTypeChange() {
		newVolume.mount_type = mountType;
	}

	async function createVolume(e: Event) {
		e.preventDefault();
		try {
			newVolume.mount_type = mountType;
			const vol = await api.createVolume(projectId, newVolume);
			volumes = [vol, ...volumes];
			showCreate = false;
			mountType = 'volume';
			newVolume = { name: '', mount_type: 'volume', remote_host: '', remote_path: '', mount_options: '', username: '', password: '' };
		} catch (e: any) {
			error = e.message;
		}
	}

	async function deleteVolume(volumeId: string) {
		if (!confirm('Delete this volume? Data stored on it may be lost.')) return;
		try {
			await api.deleteVolume(projectId, volumeId);
			volumes = volumes.filter(v => v.id !== volumeId);
		} catch (e: any) {
			error = e.message;
		}
	}

	async function attachVolume(volumeId: string) {
		if (!attachAppId || !attachPath) return;
		try {
			await api.attachVolume(projectId, volumeId, attachAppId, {
				container_path: attachPath,
				read_only: attachReadOnly,
			});
			attachingVolumeId = null;
			attachAppId = '';
			attachPath = '';
			attachReadOnly = false;
		} catch (e: any) {
			error = e.message;
		}
	}

	function typeLabel(t: string) {
		switch (t) {
			case 'nfs': return 'NFS';
			case 'cifs': return 'CIFS/SMB';
			default: return 'Local';
		}
	}

	function statusColor(status: string) {
		switch (status) {
			case 'active': return 'var(--color-success)';
			case 'pending': return 'var(--color-warning)';
			case 'error': return 'var(--color-danger)';
			default: return 'var(--color-text-muted)';
		}
	}

	function formatDate(d: string) {
		return new Date(d).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
	}
</script>

<div>
	<div class="mb-6">
		<a href="/projects/{projectId}" class="text-sm" style="color: var(--color-text-muted);">Back to project</a>
		<h2 class="text-2xl font-bold mt-1">Volumes</h2>
		<p class="text-sm mt-1" style="color: var(--color-text-muted);">Manage Docker volumes including local storage and remote NAS shares (NFS/CIFS).</p>
	</div>

	<div class="flex items-center justify-between mb-4">
		<span class="text-sm" style="color: var(--color-text-muted);">{volumes.length} volume{volumes.length !== 1 ? 's' : ''}</span>
		<button onclick={() => showCreate = !showCreate} class="px-3 py-1.5 rounded-lg text-sm font-medium cursor-pointer" style="background-color: var(--color-primary); color: var(--color-bg);">
			New Volume
		</button>
	</div>

	{#if showCreate}
		<form onsubmit={createVolume} class="rounded-lg p-4 mb-6 space-y-3" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<input type="text" bind:value={newVolume.name} placeholder="Volume name" required class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />

			<div>
				<span class="block text-xs font-medium mb-1" style="color: var(--color-text-muted);">Type</span>
				<div class="flex gap-2">
					{#each [['volume', 'Local'], ['nfs', 'NFS'], ['cifs', 'CIFS/SMB']] as [val, label]}
						<button
							type="button"
							onclick={() => { mountType = val as 'volume' | 'nfs' | 'cifs'; onMountTypeChange(); }}
							class="px-3 py-1.5 rounded-lg text-sm font-medium cursor-pointer"
							style={mountType === val ? 'background-color: var(--color-primary); color: var(--color-bg);' : 'border: 1px solid var(--color-border); color: var(--color-text-muted);'}
						>{label}</button>
					{/each}
				</div>
			</div>

			{#if mountType === 'nfs'}
				<div class="grid grid-cols-2 gap-3">
					<input type="text" bind:value={newVolume.remote_host} placeholder="NAS host (e.g. 192.168.1.100)" required class="px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />
					<input type="text" bind:value={newVolume.remote_path} placeholder="Export path (e.g. /share/media)" required class="px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />
				</div>
				<input type="text" bind:value={newVolume.mount_options} placeholder="Mount options (e.g. vers=4,soft)" class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />
			{/if}

			{#if mountType === 'cifs'}
				<div class="grid grid-cols-2 gap-3">
					<input type="text" bind:value={newVolume.remote_host} placeholder="Server (e.g. 192.168.1.100)" required class="px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />
					<input type="text" bind:value={newVolume.remote_path} placeholder="Share path (e.g. /share/media)" required class="px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />
				</div>
				<div class="grid grid-cols-2 gap-3">
					<input type="text" bind:value={newVolume.username} placeholder="Username" class="px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />
					<input type="password" bind:value={newVolume.password} placeholder="Password" class="px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />
				</div>
				<input type="text" bind:value={newVolume.mount_options} placeholder="Mount options (e.g. vers=3.0,uid=1000)" class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);" />
			{/if}

			<div class="flex gap-2">
				<button type="submit" class="px-4 py-2 rounded-lg text-sm font-medium cursor-pointer" style="background-color: var(--color-primary); color: var(--color-bg);">Create Volume</button>
				<button type="button" onclick={() => showCreate = false} class="px-4 py-2 rounded-lg text-sm cursor-pointer" style="color: var(--color-text-muted);">Cancel</button>
			</div>
		</form>
	{/if}

	<div class="space-y-3">
		{#each volumes as vol}
			<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<div class="flex items-center justify-between">
					<div>
						<span class="font-semibold font-mono">{vol.name}</span>
						<div class="flex items-center gap-3 mt-1">
							<span class="text-xs px-2 py-0.5 rounded" style="background-color: var(--color-bg); color: var(--color-text-muted);">{typeLabel(vol.mount_type)}</span>
							<span class="text-xs font-medium" style="color: {statusColor(vol.status)};">{vol.status}</span>
							{#if vol.remote_host}
								<span class="text-xs" style="color: var(--color-text-muted);">{vol.remote_host}{vol.remote_path}</span>
							{/if}
						</div>
						<div class="text-xs mt-1" style="color: var(--color-text-muted);">Created {formatDate(vol.created_at)}</div>
					</div>
					<div class="flex items-center gap-2">
						<button
							onclick={() => { attachingVolumeId = attachingVolumeId === vol.id ? null : vol.id; }}
							class="px-3 py-1.5 rounded-lg text-xs font-medium cursor-pointer"
							style="border: 1px solid var(--color-border); color: var(--color-text-muted);"
						>Mount to App</button>
						<button
							onclick={() => deleteVolume(vol.id)}
							class="px-3 py-1.5 rounded-lg text-xs font-medium cursor-pointer"
							style="border: 1px solid var(--color-danger); color: var(--color-danger);"
						>Delete</button>
					</div>
				</div>

				{#if attachingVolumeId === vol.id}
					<div class="mt-3 p-3 rounded-lg space-y-2" style="background-color: var(--color-bg); border: 1px solid var(--color-border);">
						<select bind:value={attachAppId} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-surface); border: 1px solid var(--color-border); color: var(--color-text);">
							<option value="">Select an app...</option>
							{#each apps as app}
								<option value={app.id}>{app.name}</option>
							{/each}
						</select>
						<input type="text" bind:value={attachPath} placeholder="Container path (e.g. /data)" required class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background-color: var(--color-surface); border: 1px solid var(--color-border); color: var(--color-text);" />
						<label class="flex items-center gap-2 text-sm" style="color: var(--color-text-muted);">
							<input id="attach-read-only" type="checkbox" bind:checked={attachReadOnly} />
							Read-only mount
						</label>
						<button onclick={() => attachVolume(vol.id)} class="px-3 py-1.5 rounded-lg text-xs font-medium cursor-pointer" style="background-color: var(--color-primary); color: var(--color-bg);">
							Mount
						</button>
					</div>
				{/if}
			</div>
		{/each}
		{#if volumes.length === 0}
			<p class="text-sm py-4" style="color: var(--color-text-muted);">No volumes in this project yet.</p>
		{/if}
	</div>

	{#if error}
		<div class="rounded-lg p-4 mt-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);">
			<p style="color: var(--color-danger);">{error}</p>
		</div>
	{/if}
</div>
