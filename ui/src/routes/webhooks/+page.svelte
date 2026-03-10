<script lang="ts">
	import { api } from '$lib/api';
	import { invalidateAll } from '$app/navigation';
	import type { WebhookEndpoint, WebhookDelivery } from '$lib/types';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();

	const WEBHOOK_EVENTS = [
		{ id: 'app.deployed', label: 'App Deployed' },
		{ id: 'app.stopped', label: 'App Stopped' },
		{ id: 'node.offline', label: 'Node Offline' },
		{ id: 'alert.triggered', label: 'Alert Triggered' },
		{ id: 'backup.completed', label: 'Backup Completed' },
	];

	let showForm = $state(false);
	let saving = $state(false);
	let error = $state<string | null>(null);
	let expandedId = $state<string | null>(null);
	let deliveriesCache = $state<Record<string, WebhookDelivery[]>>({});
	let deliveriesLoading = $state<Record<string, boolean>>({});
	let testingId = $state<string | null>(null);

	let form = $state({
		name: '',
		url: '',
		events: [] as string[],
	});

	function resetForm() {
		form = { name: '', url: '', events: [] };
		showForm = false;
		error = null;
	}

	function toggleEvent(id: string) {
		if (form.events.includes(id)) {
			form = { ...form, events: form.events.filter((e) => e !== id) };
		} else {
			form = { ...form, events: [...form.events, id] };
		}
	}

	async function submitForm() {
		saving = true;
		error = null;
		try {
			await api.createWebhook({
				name: form.name,
				url: form.url,
				events: form.events.length > 0 ? form.events : undefined,
			});
			resetForm();
			await invalidateAll();
		} catch (e: unknown) {
			error = e instanceof Error ? e.message : 'Failed to create webhook';
		} finally {
			saving = false;
		}
	}

	async function toggleEnabled(wh: WebhookEndpoint) {
		try {
			await api.updateWebhook(wh.id, { enabled: !wh.enabled });
			await invalidateAll();
		} catch (e: unknown) {
			alert(e instanceof Error ? e.message : 'Failed to update');
		}
	}

	async function loadDeliveries(id: string) {
		if (deliveriesCache[id]) return;
		deliveriesLoading[id] = true;
		deliveriesLoading = deliveriesLoading;
		try {
			const list = await api.webhookDeliveries(id);
			deliveriesCache = { ...deliveriesCache, [id]: list };
		} catch {
			deliveriesCache = { ...deliveriesCache, [id]: [] };
		} finally {
			deliveriesLoading[id] = false;
			deliveriesLoading = deliveriesLoading;
		}
	}

	function toggleExpand(wh: WebhookEndpoint) {
		if (expandedId === wh.id) {
			expandedId = null;
		} else {
			expandedId = wh.id;
			loadDeliveries(wh.id);
		}
	}

	async function testWebhook(id: string) {
		testingId = id;
		try {
			await api.testWebhook(id);
			await loadDeliveries(id);
			deliveriesCache = { ...deliveriesCache };
			await invalidateAll();
		} catch (e: unknown) {
			alert(e instanceof Error ? e.message : 'Test failed');
		} finally {
			testingId = null;
		}
	}

	async function deleteWebhook(id: string) {
		if (!confirm('Delete this webhook? Delivery history will be lost.')) return;
		try {
			await api.deleteWebhook(id);
			await invalidateAll();
		} catch (e: unknown) {
			alert(e instanceof Error ? e.message : 'Failed to delete');
		}
	}

	function formatDate(d: string) {
		return new Date(d).toLocaleString();
	}

	function parseEvents(s: string): string[] {
		if (!s) return [];
		try {
			const v = JSON.parse(s);
			return Array.isArray(v) ? v : [String(s)];
		} catch {
			return s.split(',').map((x) => x.trim()).filter(Boolean);
		}
	}

	function lastDelivery(wh: WebhookEndpoint): string | null {
		const list = deliveriesCache[wh.id];
		if (list && list.length > 0) return list[0].delivered_at;
		return null;
	}

	function deliveryStatusClass(status: number): string {
		if (status >= 200 && status < 300) return 'bg-emerald-500/20 text-emerald-400';
		if (status >= 400) return 'bg-red-500/20 text-red-400';
		if (status >= 300 && status < 400) return 'bg-slate-600 text-slate-400';
		return 'bg-slate-600 text-slate-400';
	}

	const webhooks = $derived((data?.data ?? []) as WebhookEndpoint[]);
</script>

<svelte:head><title>Webhooks | Hive</title></svelte:head>

<div class="max-w-5xl mx-auto">
	<div class="page-header">
		<div>
			<h2 class="page-title">Webhooks</h2>
			<p class="page-subtitle">Receive events in real time via HTTP callbacks</p>
		</div>
		<button
			class="btn btn-primary"
			onclick={() => (showForm ? resetForm() : (showForm = true))}
		>
			{showForm ? 'Cancel' : '+ Add Webhook'}
		</button>
	</div>

	{#if showForm}
		<form
			onsubmit={(e) => {
				e.preventDefault();
				submitForm();
			}}
			class="rounded-lg p-6 mb-6 bg-slate-800/50 border border-slate-700 space-y-4"
		>
			<h3 class="text-lg font-semibold text-slate-200">Add Webhook</h3>
			{#if error}
				<div class="text-sm text-red-400 bg-red-900/20 px-3 py-2 rounded">
					{error}
				</div>
			{/if}
			<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
				<div>
					<label for="wh-name">Name</label>
					<input id="wh-name" type="text" bind:value={form.name} required placeholder="Slack notifications" />
				</div>
				<div>
					<label for="wh-url">URL</label>
					<input id="wh-url" type="url" bind:value={form.url} required placeholder="https://hooks.slack.com/..." />
				</div>
			</div>
			<fieldset>
				<legend class="text-sm font-medium text-slate-300 mb-2">Events</legend>
				<div class="flex flex-wrap gap-4">
					{#each WEBHOOK_EVENTS as ev}
						<label class="flex items-center gap-2 cursor-pointer text-slate-300">
							<input
								type="checkbox"
								checked={form.events.includes(ev.id)}
								onchange={() => toggleEvent(ev.id)}
							/>
							{ev.label}
						</label>
					{/each}
				</div>
			</fieldset>
			<div class="flex justify-end gap-3">
				<button type="button" class="btn bg-slate-700 text-slate-200 border-slate-600" onclick={resetForm}>
					Cancel
				</button>
				<button type="submit" class="btn btn-primary" disabled={saving}>
					{saving ? 'Creating...' : 'Create'}
				</button>
			</div>
		</form>
	{/if}

	{#if webhooks.length === 0 && !showForm}
		<div class="rounded-lg p-8 text-center bg-slate-800/50 border border-slate-700">
			<p class="text-lg font-medium text-slate-200 mb-2">No webhooks configured</p>
			<p class="text-sm text-slate-400 mb-4">Add a webhook to receive app, node, alert, and backup events.</p>
			<button class="btn btn-primary" onclick={() => (showForm = true)}>
				+ Add Webhook
			</button>
		</div>
	{:else}
		<div class="space-y-4">
			{#each webhooks as wh}
				<div class="rounded-lg border border-slate-700 bg-slate-800/50 overflow-hidden">
					<div class="p-5 flex items-start justify-between gap-4">
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-3 mb-2 flex-wrap">
								<h3 class="font-semibold text-slate-200">{wh.name}</h3>
								<label class="flex items-center gap-2 cursor-pointer">
									<input
										type="checkbox"
										checked={wh.enabled}
										onchange={() => toggleEnabled(wh)}
									/>
									<span class="text-sm text-slate-400">Enabled</span>
								</label>
							</div>
							<p class="text-sm font-mono text-slate-400 truncate">{wh.url}</p>
							<div class="flex flex-wrap gap-2 mt-2">
								{#each parseEvents(wh.events) as ev}
									<span class="text-xs px-2 py-0.5 rounded bg-slate-600 text-slate-300">
										{ev}
									</span>
								{/each}
								{#if parseEvents(wh.events).length === 0}
									<span class="text-xs text-slate-500">No events</span>
								{/if}
							</div>
							{#if lastDelivery(wh)}
								<p class="text-xs text-slate-500 mt-2">Last delivery: {formatDate(lastDelivery(wh)!)}</p>
							{/if}
						</div>
						<div class="flex gap-2 shrink-0">
							<button
								class="btn bg-slate-700 text-slate-200 border-slate-600"
								disabled={testingId === wh.id}
								onclick={() => testWebhook(wh.id)}
							>
								{testingId === wh.id ? 'Testing...' : 'Test'}
							</button>
							<button
								class="btn bg-slate-700 text-slate-200 border-slate-600"
								onclick={() => toggleExpand(wh)}
							>
								{expandedId === wh.id ? 'Hide' : 'History'}
							</button>
							<button
								class="btn bg-red-900/40 text-red-400 border-red-800 hover:bg-red-900/60"
								onclick={() => deleteWebhook(wh.id)}
							>
								Delete
							</button>
						</div>
					</div>

					{#if expandedId === wh.id}
						<div class="border-t border-slate-700 bg-slate-900/50 p-4">
							{#if deliveriesLoading[wh.id]}
								<p class="text-sm text-slate-500">Loading...</p>
							{:else if (deliveriesCache[wh.id] ?? []).length === 0}
								<p class="text-sm text-slate-500">No deliveries yet</p>
							{:else}
								<div class="space-y-2 max-h-60 overflow-y-auto">
									{#each deliveriesCache[wh.id] ?? [] as d}
										<div class="flex items-center justify-between gap-4 text-sm py-2 border-b border-slate-700/50 last:border-0">
											<div>
												<span class="text-slate-300">{d.event_type}</span>
												<span class="text-slate-500 ml-2">{formatDate(d.delivered_at)}</span>
											</div>
											<span
												class="px-2 py-0.5 rounded text-xs font-medium {deliveryStatusClass(d.response_status)}"
											>
												{d.response_status}
											</span>
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
