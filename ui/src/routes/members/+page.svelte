<script lang="ts">
	import { api } from '$lib/api';
	import type { OrgRole } from '$lib/types';
	import { Button, EmptyState } from '$lib/components';
	import { invalidateAll } from '$app/navigation';

	let { data } = $props();

	let error = $state('');
	let showForm = $state(false);
	let editingUserId = $state('');

	let newUserId = $state('');
	let newRole = $state('viewer');
	let editRole = $state('viewer');

	const roles = ['owner', 'admin', 'member', 'viewer'] as const;

	async function invite() {
		try {
			await api.inviteMember({ user_id: newUserId, role: newRole });
			showForm = false;
			newUserId = '';
			newRole = 'viewer';
			await invalidateAll();
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
			await invalidateAll();
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
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		}
	}
</script>

<svelte:head><title>Members | Hive</title></svelte:head>

<div class="max-w-4xl mx-auto">
	<div class="page-header">
		<h2 class="page-title">Members</h2>
		<Button variant={showForm ? 'secondary' : 'primary'} onclick={() => { showForm = !showForm; newUserId = ''; }}>
			{showForm ? 'Cancel' : 'Invite Member'}
		</Button>
	</div>

	{#if error}
		<div
			class="rounded-lg p-4 mb-4"
			style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);"
		>
			<p style="color: var(--color-danger);">{error}</p>
			<button onclick={() => (error = '')} class="text-xs mt-1 underline" style="color: var(--color-danger);"
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
		{#each data.members ?? [] as m}
			<div
				class="rounded-lg p-4 flex flex-col sm:flex-row sm:items-center justify-between gap-3"
				style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
			>
				<div class="min-w-0">
					<p class="text-sm font-medium font-mono truncate">{m.user_id}</p>
					<p class="text-xs" style="color: var(--color-text-muted);">
						Added {new Date(m.created_at).toLocaleDateString()}
					</p>
				</div>
				{#if editingUserId === m.user_id}
					<div class="flex items-center gap-2 flex-wrap">
						<select
							bind:value={editRole}
							class="rounded px-3 py-2 text-sm"
							style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
						>
							{#each roles as r}
								<option value={r}>{r}</option>
							{/each}
						</select>
						<button
							onclick={saveRole}
							class="px-3 py-2 rounded text-sm font-medium text-white"
							style="background-color: var(--color-primary);"
						>
							Save
						</button>
						<button
							onclick={cancelEdit}
							class="px-3 py-2 rounded text-sm"
							style="border: 1px solid var(--color-border);"
						>
							Cancel
						</button>
					</div>
				{:else}
					<div class="flex items-center gap-2 flex-wrap">
						<span
							class="px-2 py-0.5 rounded text-xs font-medium capitalize"
							style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
						>
							{m.role}
						</span>
						<button
							onclick={() => startEdit(m)}
							class="px-3 py-2 rounded text-sm"
							style="border: 1px solid var(--color-border);"
						>
							Edit
						</button>
						<button
							onclick={() => remove(m.user_id)}
							class="px-3 py-2 rounded text-sm font-medium"
							style="border: 1px solid var(--color-danger); color: var(--color-danger);"
						>
							Remove
						</button>
					</div>
				{/if}
			</div>
		{/each}
		{#if (data.members ?? []).length === 0 && !showForm}
			<EmptyState title="No members yet" description="Invite members to collaborate on this organization." />
		{/if}
	</div>
</div>
