<script lang="ts">
	import { api } from '$lib/api';
	import { Button, EmptyState } from '$lib/components';
	import { invalidateAll } from '$app/navigation';

	let { data } = $props();

	let error = $state('');
	let showForm = $state(false);
	let testing = $state('');

	let newType = $state('discord');
	let newConfig = $state<Record<string, string>>({});

	const configFields: Record<string, { label: string; key: string; placeholder: string }[]> = {
		discord: [{ label: 'Webhook URL', key: 'webhook_url', placeholder: 'https://discord.com/api/webhooks/...' }],
		slack: [{ label: 'Webhook URL', key: 'webhook_url', placeholder: 'https://hooks.slack.com/services/...' }],
		webhook: [{ label: 'URL', key: 'url', placeholder: 'https://example.com/webhook' }],
		gotify: [
			{ label: 'Server URL', key: 'url', placeholder: 'https://gotify.example.com' },
			{ label: 'App Token', key: 'token', placeholder: 'AxxxxxxxxxxxxxxE' },
		],
		email: [
			{ label: 'SMTP Host', key: 'smtp_host', placeholder: 'smtp.gmail.com' },
			{ label: 'SMTP Port', key: 'smtp_port', placeholder: '587' },
			{ label: 'SMTP User', key: 'smtp_user', placeholder: 'user@gmail.com' },
			{ label: 'SMTP Password', key: 'smtp_pass', placeholder: '...' },
			{ label: 'To Address', key: 'to', placeholder: 'admin@example.com' },
		],
		resend: [
			{ label: 'API Key', key: 'api_key', placeholder: 're_xxxxxxxxxxxxxxxxxxxx' },
			{ label: 'From Address', key: 'from_address', placeholder: 'hive@yourdomain.com' },
			{ label: 'To Address', key: 'to_address', placeholder: 'admin@example.com' },
		],
	};

	async function create() {
		try {
			await api.createNotificationChannel({ type: newType, config: newConfig });
			showForm = false;
			newConfig = {};
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		}
	}

	async function remove(id: string) {
		if (!confirm('Delete this notification channel?')) return;
		try {
			await api.deleteNotificationChannel(id);
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		}
	}

	async function test(id: string) {
		testing = id;
		try {
			await api.testNotificationChannel(id);
		} catch (e: any) {
			error = e.message;
		}
		testing = '';
	}

	function typeIcon(type: string): string {
		switch (type) {
			case 'discord': return '💬';
			case 'slack': return '📨';
			case 'webhook': return '🔗';
			case 'email': return '✉️';
			case 'gotify': return '🔔';
			case 'resend': return '📧';
			default: return '📢';
		}
	}
</script>

<svelte:head><title>Notifications | Hive</title></svelte:head>

<div class="max-w-4xl mx-auto p-6">
	<div class="page-header">
		<h2 class="page-title">Notification Channels</h2>
		<Button variant={showForm ? 'secondary' : 'primary'} onclick={() => { showForm = !showForm; newConfig = {}; }}>
			{showForm ? 'Cancel' : 'Add Channel'}
		</Button>
	</div>

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);">
			<p style="color: var(--color-danger);">{error}</p>
			<button onclick={() => error = ''} class="text-xs mt-1 underline" style="color: var(--color-danger);">Dismiss</button>
		</div>
	{/if}

	{#if showForm}
		<div class="rounded-lg p-5 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<div class="mb-4">
				<span class="block text-sm font-medium mb-2">Type</span>
				<div class="flex gap-2 flex-wrap">
					{#each Object.keys(configFields) as t}
						<button onclick={() => { newType = t; newConfig = {}; }}
							class="px-3 py-1.5 rounded text-sm"
							style="border: 1px solid {newType === t ? 'var(--color-primary)' : 'var(--color-border)'}; background-color: {newType === t ? 'var(--color-primary)' : 'transparent'}; color: {newType === t ? 'white' : 'inherit'};">
							{typeIcon(t)} {t}
						</button>
					{/each}
				</div>
			</div>

			{#each configFields[newType] ?? [] as field}
				<div class="mb-3">
					<label for={'cfg-' + field.key} class="block text-sm mb-1" style="color: var(--color-text-muted);">{field.label}</label>
					<input id={'cfg-' + field.key} type={field.key.includes('pass') || field.key.includes('token') ? 'password' : 'text'}
						placeholder={field.placeholder}
						value={newConfig[field.key] ?? ''}
						oninput={(e) => { newConfig = { ...newConfig, [field.key]: (e.target as HTMLInputElement).value }; }}
						class="w-full rounded px-3 py-2 text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
			{/each}

			<button onclick={create}
				class="px-4 py-2 rounded text-sm font-medium text-white mt-2"
				style="background-color: var(--color-primary);">
				Create Channel
			</button>
		</div>
	{/if}

	<div class="space-y-3">
		{#each data.channels ?? [] as ch}
			<div class="rounded-lg p-4 flex items-center justify-between" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<div class="flex items-center gap-3">
					<span class="text-xl">{typeIcon(ch.type)}</span>
					<div>
						<p class="text-sm font-medium capitalize">{ch.type}</p>
						<p class="text-xs" style="color: var(--color-text-muted);">Added {new Date(ch.created_at).toLocaleDateString()}</p>
					</div>
				</div>
				<div class="flex gap-2">
					<button onclick={() => test(ch.id)} disabled={testing === ch.id}
						class="px-3 py-1 rounded text-xs font-medium" style="border: 1px solid var(--color-border);">
						{testing === ch.id ? 'Sending...' : 'Test'}
					</button>
					<button onclick={() => remove(ch.id)}
						class="px-3 py-1 rounded text-xs font-medium" style="border: 1px solid var(--color-danger); color: var(--color-danger);">
						Delete
					</button>
				</div>
			</div>
		{/each}
		{#if (data.channels ?? []).length === 0 && !showForm}
			<EmptyState title="No notification channels configured" description="Get notified about deployments, backups, and health events." />
		{/if}
	</div>
</div>
