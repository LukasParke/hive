<script lang="ts">
	import { api } from '$lib/api';
	import type { DNSRecord } from '$lib/types';
	import { Button, EmptyState } from '$lib/components';
	import { invalidateAll } from '$app/navigation';

	let { data } = $props();

	let recordsByProvider = $state<Record<string, DNSRecord[]>>({});
	let expandedProvider = $state<string | null>(null);
	let error = $state('');
	let success = $state('');
	let showForm = $state(false);
	let editingProviderId = $state('');
	let testing = $state('');
	let busy = $state('');
	let manualRecordForm = $state<Record<string, { domain: string; record_type: string; value: string; proxied: boolean }>>({});

	let form = $state({
		name: '',
		type: 'cloudflare',
		config: {} as Record<string, string>,
		is_default: false,
	});

	let editForm = $state({
		name: '',
		type: 'cloudflare',
		config: {} as Record<string, string>,
		is_default: false,
	});

	const configFields: Record<string, { label: string; key: string; placeholder: string }[]> = {
		cloudflare: [
			{ label: 'API Token', key: 'api_token', placeholder: 'Cloudflare API token' },
			{ label: 'Zone ID', key: 'zone_id', placeholder: 'Zone ID from Cloudflare dashboard' },
		],
		route53: [
			{ label: 'Access Key', key: 'access_key', placeholder: 'AWS access key' },
			{ label: 'Secret Key', key: 'secret_key', placeholder: 'AWS secret key' },
			{ label: 'Hosted Zone ID', key: 'zone_id', placeholder: 'Route53 hosted zone ID' },
		],
	};

	async function loadRecords(providerId: string) {
		try {
			const records = await api.listDNSRecords(providerId);
			recordsByProvider = { ...recordsByProvider, [providerId]: records };
		} catch (e: any) {
			error = e.message;
		}
	}

	function toggleRecords(providerId: string) {
		if (expandedProvider === providerId) {
			expandedProvider = null;
		} else {
			expandedProvider = providerId;
			if (!recordsByProvider[providerId]) {
				loadRecords(providerId);
			}
		}
	}

	async function create() {
		const required = configFields[form.type] ?? [];
		for (const field of required) {
			if (!form.config[field.key]?.trim()) {
				error = `${field.label} is required`;
				return;
			}
		}
		busy = 'create-provider';
		try {
			await api.createDNSProvider({
				name: form.name,
				type: form.type,
				config: form.config,
				is_default: form.is_default,
			});
			showForm = false;
			form = { name: '', type: 'cloudflare', config: {}, is_default: false };
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		} finally {
			busy = '';
		}
	}

	async function removeProvider(id: string) {
		if (!confirm('Delete this DNS provider? All associated records will be removed.')) return;
		busy = `delete-provider:${id}`;
		try {
			await api.deleteDNSProvider(id);
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		} finally {
			busy = '';
		}
	}

	function beginEditProvider(provider: any) {
		editingProviderId = provider.id;
		editForm = {
			name: provider.name,
			type: provider.type,
			config: {},
			is_default: !!provider.is_default,
		};
	}

	async function updateProvider(id: string) {
		const required = configFields[editForm.type] ?? [];
		for (const field of required) {
			if (!editForm.config[field.key]?.trim()) {
				error = `${field.label} is required`;
				return;
			}
		}
		busy = `update-provider:${id}`;
		try {
			await api.updateDNSProvider(id, editForm);
			editingProviderId = '';
			await invalidateAll();
			success = 'Provider updated.';
		} catch (e: any) {
			error = e.message;
		} finally {
			busy = '';
		}
	}

	async function removeRecord(providerId: string, recordId: string) {
		if (!confirm('Delete this DNS record?')) return;
		busy = `delete-record:${recordId}`;
		try {
			await api.deleteDNSRecord(providerId, recordId);
			await loadRecords(providerId);
		} catch (e: any) {
			error = e.message;
		} finally {
			busy = '';
		}
	}

	async function createManualRecord(providerId: string) {
		const rec = manualRecordForm[providerId];
		if (!rec?.domain || !rec?.value) {
			error = 'Domain and value are required';
			return;
		}
		if (!/^(\*\.)?[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/.test(rec.domain.trim())) {
			error = 'Enter a valid domain (example.com or *.example.com)';
			return;
		}
		busy = `create-record:${providerId}`;
		try {
			await api.createDNSRecord(providerId, rec);
			manualRecordForm = {
				...manualRecordForm,
				[providerId]: { domain: '', record_type: 'A', value: '', proxied: true },
			};
			await loadRecords(providerId);
			success = 'DNS record created.';
		} catch (e: any) {
			error = e.message;
		} finally {
			busy = '';
		}
	}

	async function testProvider(id: string) {
		testing = id;
		busy = `test-provider:${id}`;
		error = '';
		success = '';
		try {
			const result = await api.testDNSProvider(id) as { status: string; error?: string; message?: string; record_count?: number };
			if (result.status === 'ok') {
				success = result.message || 'Connection successful';
				if (result.record_count !== undefined) {
					success += ` (${result.record_count} existing records found)`;
				}
			} else {
				error = result.error ?? 'Test failed';
			}
		} catch (e: any) {
			error = e.message;
		}
		testing = '';
		busy = '';
	}

	function typeIcon(type: string): string {
		switch (type) {
			case 'cloudflare': return '☁️';
			case 'route53': return '🌐';
			default: return '🔗';
		}
	}

	function formatDate(d: string) {
		return new Date(d).toLocaleDateString();
	}
</script>

<svelte:head><title>DNS Providers | Hive</title></svelte:head>

<div class="max-w-5xl mx-auto">
	<div class="page-header">
		<div>
			<h2 class="page-title">DNS Providers</h2>
			<p class="page-subtitle">Manage DNS providers for automatic SSL certificate validation and DNS record management.</p>
		</div>
		<Button variant={showForm ? 'secondary' : 'primary'} onclick={() => { showForm = !showForm; form = { name: '', type: 'cloudflare', config: {}, is_default: false }; }}>
			{showForm ? 'Cancel' : 'Add DNS Provider'}
		</Button>
	</div>

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);">
			<p style="color: var(--color-danger);">{error}</p>
			<button onclick={() => error = ''} class="text-xs mt-1 underline" style="color: var(--color-danger);">Dismiss</button>
		</div>
	{/if}
	{#if success}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(34, 197, 94, 0.1); border: 1px solid var(--color-success);">
			<p style="color: var(--color-success);">{success}</p>
			<button onclick={() => success = ''} class="text-xs mt-1 underline" style="color: var(--color-success);">Dismiss</button>
		</div>
	{/if}

	{#if showForm}
		<form onsubmit={(e) => { e.preventDefault(); create(); }}
			class="rounded-lg p-6 mb-6 space-y-4"
			style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="font-semibold text-lg">Add DNS Provider</h3>

			<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
				<div>
					<label for="dns-name" class="block text-sm font-medium mb-1">Name</label>
					<input id="dns-name" bind:value={form.name} required placeholder="My Cloudflare"
						class="w-full px-3 py-2 rounded text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
				<div>
					<label for="dns-type" class="block text-sm font-medium mb-1">Type</label>
					<select id="dns-type" bind:value={form.type}
						class="w-full px-3 py-2 rounded text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border);">
						<option value="cloudflare">Cloudflare</option>
						<option value="route53">Route53 (coming soon)</option>
					</select>
				</div>
			</div>

			{#each configFields[form.type] ?? [] as field}
				<div>
					<label for={'cfg-' + field.key} class="block text-sm font-medium mb-1">{field.label}</label>
					<input id={'cfg-' + field.key}
						type={field.key.includes('secret') || field.key.includes('token') ? 'password' : 'text'}
						value={form.config[field.key] ?? ''}
						oninput={(e) => { form.config = { ...form.config, [field.key]: (e.target as HTMLInputElement).value }; }}
						placeholder={field.placeholder}
						class="w-full px-3 py-2 rounded text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border);" />
				</div>
			{/each}

			<div class="flex items-center gap-2">
				<input type="checkbox" id="is-default" bind:checked={form.is_default}
					class="rounded" />
				<label for="is-default" class="text-sm">Set as default provider</label>
			</div>

			<div class="flex justify-end">
				<button type="submit" class="px-4 py-2 rounded-lg text-sm font-medium"
					disabled={busy === 'create-provider'}
					style="background-color: var(--color-primary); color: white;">
					{busy === 'create-provider' ? 'Adding...' : 'Add Provider'}
				</button>
			</div>
		</form>
	{/if}

	{#if (data.providers ?? []).length === 0}
		<EmptyState title="No DNS providers configured" description="Add a Cloudflare or Route53 provider to enable automatic DNS-based SSL certificate validation." />
	{:else}
		<div class="space-y-4">
			{#each data.providers ?? [] as provider}
				<div class="rounded-lg p-4 md:p-5" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
					<div class="flex flex-col sm:flex-row sm:items-start justify-between gap-3">
						<div class="min-w-0">
							<div class="flex items-center gap-2 mb-1 flex-wrap">
								<h3 class="font-semibold text-base md:text-lg">{provider.name}</h3>
								<span class="text-xs px-2 py-0.5 rounded-full font-medium"
									style="background-color: var(--color-bg); color: var(--color-text-muted);">
									{typeIcon(provider.type)} {provider.type}
								</span>
								{#if provider.is_default}
									<span class="text-xs px-2 py-0.5 rounded-full"
										style="background-color: var(--color-primary); color: white;">
										Default
									</span>
								{/if}
							</div>
							<p class="text-xs" style="color: var(--color-text-muted);">Added {formatDate(provider.created_at)}</p>
						</div>
						<div class="flex gap-2 flex-wrap shrink-0">
							<button class="px-3 py-2 rounded text-sm"
								style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
								onclick={() => toggleRecords(provider.id)}>
								{expandedProvider === provider.id ? 'Hide' : 'Show'} Records
							</button>
							<button class="px-3 py-2 rounded text-sm"
								style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
								onclick={() => testProvider(provider.id)}
								disabled={testing === provider.id || busy.startsWith('delete-provider:')}>
								{testing === provider.id ? 'Testing...' : 'Test'}
							</button>
							<button class="px-3 py-2 rounded text-sm"
								style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
								onclick={() => beginEditProvider(provider)}
								disabled={busy.startsWith('delete-provider:')}>
								Edit
							</button>
							<button class="px-3 py-2 rounded text-sm"
								style="background-color: var(--color-danger); color: white;"
								onclick={() => removeProvider(provider.id)}
								disabled={busy === `delete-provider:${provider.id}`}>
								{busy === `delete-provider:${provider.id}` ? 'Deleting...' : 'Delete'}
							</button>
						</div>
					</div>
					{#if editingProviderId === provider.id}
						<div class="mt-4 rounded p-4" style="background-color: var(--color-bg); border: 1px solid var(--color-border);">
							<p class="text-sm font-medium mb-2">Edit Provider</p>
							<p class="text-xs mb-3" style="color: var(--color-text-muted);">Re-enter config fields to rotate or update credentials.</p>
							<div class="grid grid-cols-1 md:grid-cols-2 gap-3">
								<input bind:value={editForm.name} placeholder="Provider name" class="px-3 py-2 rounded text-sm" style="background-color: var(--color-surface); border: 1px solid var(--color-border);" />
								<select bind:value={editForm.type} class="px-3 py-2 rounded text-sm" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
									<option value="cloudflare">Cloudflare</option>
									<option value="route53">Route53</option>
								</select>
							</div>
							<div class="mt-3 space-y-2">
								{#each configFields[editForm.type] ?? [] as field}
									<input
										type={field.key.includes('secret') || field.key.includes('token') ? 'password' : 'text'}
										value={editForm.config[field.key] ?? ''}
										oninput={(e) => { editForm.config = { ...editForm.config, [field.key]: (e.target as HTMLInputElement).value }; }}
										placeholder={field.placeholder}
										class="w-full px-3 py-2 rounded text-sm"
										style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
									/>
								{/each}
							</div>
							<div class="mt-3 flex items-center gap-2">
								<input type="checkbox" id={'edit-default-' + provider.id} bind:checked={editForm.is_default} class="rounded" />
								<label for={'edit-default-' + provider.id} class="text-sm">Set as default</label>
							</div>
							<div class="mt-3 flex gap-2">
								<button class="px-3 py-2 rounded text-sm" style="background-color: var(--color-primary); color: white;" onclick={() => updateProvider(provider.id)} disabled={busy === `update-provider:${provider.id}`}>
									{busy === `update-provider:${provider.id}` ? 'Saving...' : 'Save'}
								</button>
								<button class="px-3 py-2 rounded text-sm" style="border: 1px solid var(--color-border);" onclick={() => editingProviderId = ''}>Cancel</button>
							</div>
						</div>
					{/if}
					{#if expandedProvider === provider.id}
						<div class="mt-4 pt-4" style="border-top: 1px solid var(--color-border);">
							<h4 class="text-sm font-medium mb-2">DNS Records</h4>
							<div class="mb-3 rounded p-3" style="background-color: var(--color-bg); border: 1px solid var(--color-border);">
								<p class="text-xs font-medium mb-2">Create Record</p>
								<div class="grid grid-cols-1 md:grid-cols-4 gap-2">
									<input
										placeholder="Domain"
										value={manualRecordForm[provider.id]?.domain ?? ''}
										oninput={(e) => manualRecordForm = { ...manualRecordForm, [provider.id]: { domain: (e.target as HTMLInputElement).value, record_type: manualRecordForm[provider.id]?.record_type ?? 'A', value: manualRecordForm[provider.id]?.value ?? '', proxied: manualRecordForm[provider.id]?.proxied ?? true } }}
										class="px-3 py-2 rounded text-sm"
										style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
									/>
									<select
										value={manualRecordForm[provider.id]?.record_type ?? 'A'}
										onchange={(e) => manualRecordForm = { ...manualRecordForm, [provider.id]: { domain: manualRecordForm[provider.id]?.domain ?? '', record_type: (e.target as HTMLSelectElement).value, value: manualRecordForm[provider.id]?.value ?? '', proxied: manualRecordForm[provider.id]?.proxied ?? true } }}
										class="px-3 py-2 rounded text-sm"
										style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
									>
										<option value="A">A</option>
										<option value="AAAA">AAAA</option>
										<option value="CNAME">CNAME</option>
									</select>
									<input
										placeholder="Value"
										value={manualRecordForm[provider.id]?.value ?? ''}
										oninput={(e) => manualRecordForm = { ...manualRecordForm, [provider.id]: { domain: manualRecordForm[provider.id]?.domain ?? '', record_type: manualRecordForm[provider.id]?.record_type ?? 'A', value: (e.target as HTMLInputElement).value, proxied: manualRecordForm[provider.id]?.proxied ?? true } }}
										class="px-3 py-2 rounded text-sm"
										style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
									/>
									<label class="flex items-center gap-2 text-xs px-2">
										<input
											type="checkbox"
											checked={manualRecordForm[provider.id]?.proxied ?? true}
											onchange={(e) => manualRecordForm = { ...manualRecordForm, [provider.id]: { domain: manualRecordForm[provider.id]?.domain ?? '', record_type: manualRecordForm[provider.id]?.record_type ?? 'A', value: manualRecordForm[provider.id]?.value ?? '', proxied: (e.target as HTMLInputElement).checked } }}
										/>
										Proxied
									</label>
									<button class="px-3 py-2 rounded text-sm" style="background-color: var(--color-success); color: white;" onclick={() => createManualRecord(provider.id)} disabled={busy === `create-record:${provider.id}`}>
										{busy === `create-record:${provider.id}` ? 'Creating...' : 'Create'}
									</button>
								</div>
							</div>
							{#if recordsByProvider[provider.id] === undefined}
								<p class="text-sm" style="color: var(--color-text-muted);">Loading...</p>
							{:else if recordsByProvider[provider.id]?.length === 0}
								<p class="text-sm" style="color: var(--color-text-muted);">No managed records</p>
							{:else}
								<div class="space-y-2">
									{#each recordsByProvider[provider.id] ?? [] as record}
										<div class="flex flex-col sm:flex-row sm:items-center justify-between gap-2 py-2 px-3 rounded text-sm"
											style="background-color: var(--color-bg);">
											<div class="min-w-0">
												<span class="font-mono text-xs sm:text-sm break-all">{record.domain}</span>
												<span class="ml-2 text-xs" style="color: var(--color-text-muted);">{record.record_type}</span>
												<span class="ml-2 text-xs sm:text-sm break-all" style="color: var(--color-text-muted);">→ {record.value}</span>
											</div>
											<button class="px-3 py-2 rounded text-sm shrink-0 self-end sm:self-auto" style="color: var(--color-danger);"
												onclick={() => removeRecord(provider.id, record.id)}
												disabled={busy === `delete-record:${record.id}`}>
												{busy === `delete-record:${record.id}` ? 'Deleting...' : 'Delete'}
											</button>
										</div>
									{/each}
								</div>
							{/if}
						</div>
					{/if}
				</div>
			{/each}
		</div>
	{/if}
</div>
