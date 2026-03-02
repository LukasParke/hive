<script lang="ts">
	import { api, type OrgRole } from '$lib/api';
	import { onMount } from 'svelte';

	let members = $state<OrgRole[]>([]);
	let error = $state('');
	let showForm = $state(false);
	let editingUserId = $state('');

	let newUserId = $state('');
	let newRole = $state('viewer');
	let editRole = $state('viewer');

	const roles = ['owner', 'admin', 'deployer', 'viewer'] as const;

	onMount(load);

	async function load() {
		try {
			members = await api.listOrgMembers();
		} catch (e: any) {
			error = e.message;
		}
	}

	async function invite() {
		try {
			await api.inviteMember({ user_id: newUserId, role: newRole });
			showForm = false;
			newUserId = '';
			newRole = 'viewer';
			await load();
		} catch (e: any) {
			error = e.message;
		}
	}

	function startEdit(m: OrgRole) {
		editingUserId = m.user_id;
		editRole = m.role;
	}

	async function saveRole() {
		try {
			await api.updateMemberRole(editingUserId, editRole);
			editingUserId = '';
			await load();
		} catch (e: any) {
			error = e.message;
		}
	}

	function cancelEdit() {
		editingUserId = '';
	}

	async function remove(userId: string) {
		if (!confirm(`Remove this member?`)) return;
		try {
			await api.removeMember(userId);
			await load();
		} catch (e: any) {
			error = e.message;
		}
	}
</script>

<div class="max-w-4xl mx-auto p-6">
	<div class="flex items-center justify-between mb-6">
		<h2 class="text-2xl font-bold">Members</h2>
		<button
			onclick={() => {
				showForm = !showForm;
				newUserId = '';
			}}
			class="px-4 py-2 rounded text-sm font-medium text-white"
			style="background-color: var(--color-primary);"
		>
			{showForm ? 'Cancel' : 'Invite Member'}
		</button>
	</div>

	{#if error}
		<div
			class="rounded-lg p-4 mb-4"
			style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid #ef4444;"
		>
			<p style="color: #ef4444;">{error}</p>
			<button onclick={() => (error = '')} class="text-xs mt-1 underline" style="color: #ef4444;"
				>Dismiss</button
			>
		</div>
	{/if}

	{#if showForm}
		<div
			class="rounded-lg p-5 mb-6"
			style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
		>
			<div class="mb-4">
				<label for="user-id" class="block text-sm mb-1" style="color: var(--color-text-muted);"
					>User ID</label
				>
				<input
					id="user-id"
					type="text"
					bind:value={newUserId}
					placeholder="user-id-from-auth"
					class="w-full rounded px-3 py-2 text-sm"
					style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
				/>
			</div>
			<div class="mb-4">
				<label class="block text-sm mb-1" style="color: var(--color-text-muted);">Role</label>
				<select
					bind:value={newRole}
					class="rounded px-3 py-2 text-sm"
					style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
				>
					{#each roles as r}
						<option value={r}>{r}</option>
					{/each}
				</select>
			</div>
			<button
				onclick={invite}
				class="px-4 py-2 rounded text-sm font-medium text-white"
				style="background-color: var(--color-primary);"
			>
				Invite
			</button>
		</div>
	{/if}

	<div class="space-y-3">
		{#each members as m}
			<div
				class="rounded-lg p-4 flex items-center justify-between"
				style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
			>
				<div>
					<p class="text-sm font-medium font-mono">{m.user_id}</p>
					<p class="text-xs" style="color: var(--color-text-muted);">
						Added {new Date(m.created_at).toLocaleDateString()}
					</p>
				</div>
				{#if editingUserId === m.user_id}
					<div class="flex items-center gap-2">
						<select
							bind:value={editRole}
							class="rounded px-2 py-1 text-xs"
							style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
						>
							{#each roles as r}
								<option value={r}>{r}</option>
							{/each}
						</select>
						<button
							onclick={saveRole}
							class="px-2 py-1 rounded text-xs font-medium text-white"
							style="background-color: var(--color-primary);"
						>
							Save
						</button>
						<button
							onclick={cancelEdit}
							class="px-2 py-1 rounded text-xs"
							style="border: 1px solid var(--color-border);"
						>
							Cancel
						</button>
					</div>
				{:else}
					<div class="flex items-center gap-2">
						<span
							class="px-2 py-0.5 rounded text-xs font-medium capitalize"
							style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
						>
							{m.role}
						</span>
						<button
							onclick={() => startEdit(m)}
							class="px-2 py-1 rounded text-xs"
							style="border: 1px solid var(--color-border);"
						>
							Edit
						</button>
						<button
							onclick={() => remove(m.user_id)}
							class="px-2 py-1 rounded text-xs font-medium"
							style="border: 1px solid #ef4444; color: #ef4444;"
						>
							Remove
						</button>
					</div>
				{/if}
			</div>
		{/each}
		{#if members.length === 0 && !showForm}
			<div class="text-center py-12">
				<p class="text-lg mb-2" style="color: var(--color-text-muted);">No members yet</p>
				<p class="text-sm" style="color: var(--color-text-muted);">Invite members to collaborate on this organization</p>
			</div>
		{/if}
	</div>
</div>
