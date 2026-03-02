<script lang="ts">
	import { api, type DNSProvider, type DNSRecord } from '$lib/api';
	import { onMount } from 'svelte';

	let providers = $state<DNSProvider[]>([]);
	let recordsByProvider = $state<Record<string, DNSRecord[]>>({});
	let expandedProvider = $state<string | null>(null);
	let loading = $state(true);
	let error = $state('');
	let showForm = $state(false);
	let testing = $state('');

	let form = $state({
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

	onMount(load);

	async function load() {
		loading = true;
		try {
			providers = await api.listDNSProviders();
		} catch (e: any) {
			error = e.message;
		}
		loading = false;
	}

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
		try {
			await api.createDNSProvider({
				name: form.name,
				type: form.type,
				config: form.config,
				is_default: form.is_default,
			});
			showForm = false;
			form = { name: '', type: 'cloudflare', config: {}, is_default: false };
			await load();
		} catch (e: any) {
			error = e.message;
		}
	}

	async function removeProvider(id: string) {
		if (!confirm('Delete this DNS provider? All associated records will be removed.')) return;
		try {
			await api.deleteDNSProvider(id);
			await load();
		} catch (e: any) {
			error = e.message;
		}
	}

	async function removeRecord(providerId: string, recordId: string) {
		if (!confirm('Delete this DNS record?')) return;
		try {
			await api.deleteDNSRecord(providerId, recordId);
			await loadRecords(providerId);
		} catch (e: any) {
			error = e.message;
		}
	}

	async function testProvider(id: string) {
		testing = id;
		try {
			const result = await api.testDNSProvider(id);
			if (result.status === 'ok') {
				error = '';
			} else {
				error = (result as { error?: string }).error ?? 'Test failed';
			}
		} catch (e: any) {
			error = e.message;
		}
		testing = '';
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

<div class="max-w-5xl mx-auto">
	<div class="flex items-center justify-between mb-6">
		<div>
			<h2 class="text-2xl font-bold">DNS Providers</h2>
			<p class="text-sm mt-1" style="color: var(--color-text-muted);">
				Manage DNS providers for automatic SSL certificate validation and DNS record management.
			</p>
		</div>
		<button class="px-4 py-2 rounded-lg text-sm font-medium"
			style="background-color: var(--color-primary); color: white;"
			onclick={() => { showForm = !showForm; form = { name: '', type: 'cloudflare', config: {}, is_default: false }; }}>
			{showForm ? 'Cancel' : '+ Add DNS Provider'}
		</button>
	</div>

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid #ef4444;">
			<p style="color: #ef4444;">{error}</p>
			<button onclick={() => error = ''} class="text-xs mt-1 underline" style="color: #ef4444;">Dismiss</button>
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
					style="background-color: var(--color-primary); color: white;">
					Add Provider
				</button>
			</div>
		</form>
	{/if}

	{#if loading}
		<p style="color: var(--color-text-muted);">Loading...</p>
	{:else if providers.length === 0}
		<div class="rounded-lg p-8 text-center" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<p class="text-lg font-medium mb-2">No DNS providers configured</p>
			<p class="text-sm" style="color: var(--color-text-muted);">
				Add a Cloudflare or Route53 provider to enable automatic DNS-based SSL certificate validation.
			</p>
		</div>
	{:else}
		<div class="space-y-4">
			{#each providers as provider}
				<div class="rounded-lg p-5" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
					<div class="flex items-start justify-between">
						<div>
							<div class="flex items-center gap-3 mb-1">
								<h3 class="font-semibold text-lg">{provider.name}</h3>
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
						<div class="flex gap-2">
							<button class="px-3 py-1.5 rounded text-sm"
								style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
								onclick={() => toggleRecords(provider.id)}>
								{expandedProvider === provider.id ? 'Hide' : 'Show'} Records
							</button>
							<button class="px-3 py-1.5 rounded text-sm"
								style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
								onclick={() => testProvider(provider.id)}
								disabled={testing === provider.id}>
								{testing === provider.id ? 'Testing...' : 'Test'}
							</button>
							<button class="px-3 py-1.5 rounded text-sm"
								style="background-color: #ef4444; color: white;"
								onclick={() => removeProvider(provider.id)}>
								Delete
							</button>
						</div>
					</div>
					{#if expandedProvider === provider.id}
						<div class="mt-4 pt-4" style="border-top: 1px solid var(--color-border);">
							<h4 class="text-sm font-medium mb-2">DNS Records</h4>
							{#if recordsByProvider[provider.id] === undefined}
								<p class="text-sm" style="color: var(--color-text-muted);">Loading...</p>
							{:else if recordsByProvider[provider.id]?.length === 0}
								<p class="text-sm" style="color: var(--color-text-muted);">No managed records</p>
							{:else}
								<div class="space-y-2">
									{#each recordsByProvider[provider.id] ?? [] as record}
										<div class="flex items-center justify-between py-2 px-3 rounded text-sm"
											style="background-color: var(--color-bg);">
											<div>
												<span class="font-mono">{record.domain}</span>
												<span class="ml-2 text-xs" style="color: var(--color-text-muted);">{record.record_type}</span>
												<span class="ml-2" style="color: var(--color-text-muted);">→ {record.value}</span>
											</div>
											<button class="px-2 py-1 rounded text-xs" style="color: #ef4444;"
												onclick={() => removeRecord(provider.id, record.id)}>
												Delete
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
