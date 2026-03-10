<script lang="ts">
	import { api } from '$lib/api';
	import { goto, invalidateAll } from '$app/navigation';
	import type { ServiceUpdateStatus } from '$lib/types';
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';

	let { data } = $props();

	let error = $state('');
	let activeTab = $state<'overview' | 'tasks' | 'deployments' | 'events' | 'links' | 'previews'>('overview');
	let updateStatus = $state<ServiceUpdateStatus | null>(null);
	let updateLoading = $state(false);
	let showDeleteConfirm = $state(false);
	let logLines = $state<string[]>([]);
	let scaleInput = $state(1);
	let actionLoading = $state('');
	let editingDomain = $state(false);
	let domainDraft = $state('');
	let domainProvisioning = $state(false);

	let logSocket: WebSocket | null = null;
	const deployInProgress = $derived(data.app?.status === 'deploying' || data.app?.status === 'building');

	$effect(() => {
		if (data.app) scaleInput = data.app.replicas;
	});

	onMount(() => {
		checkForImageUpdate();
	});

	async function checkForImageUpdate() {
		if (!data.app) return;
		try {
			const statuses = await api.updatesServices();
			const serviceName = 'hive-app-' + data.app.name;
			const match = statuses.find((s: ServiceUpdateStatus) => s.service_name === serviceName || s.app_id === data.appId);
			if (match) updateStatus = match;
		} catch { /* update check optional */ }
	}

	async function applyImageUpdate() {
		if (!updateStatus || !data.app) return;
		updateLoading = true;
		try {
			const serviceName = 'hive-app-' + data.app.name;
			await api.applyServiceUpdate(serviceName);
			updateStatus = { ...updateStatus, update_available: false };
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		}
		updateLoading = false;
	}

	$effect(() => {
		return () => {
			logSocket?.close();
		};
	});

	function connectContainerLogs() {
		if (!data.app) return;
		logSocket?.close();
		logLines = [];
		const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
		const url = `${proto}//${location.host}/ws/logs/${data.appId}?tail=200`;
		logSocket = new WebSocket(url);
		logSocket.onmessage = (e) => {
			try {
				const msg = JSON.parse(e.data);
				if (msg.type === 'log' && msg.data) {
					logLines = [...logLines.slice(-999), msg.data];
				}
			} catch {
				logLines = [...logLines.slice(-999), e.data];
			}
		};
		logSocket.onerror = () => {
			logSocket?.close();
			logSocket = null;
		};
	}

	$effect(() => {
		if (typeof window !== 'undefined' && activeTab === 'overview' && data.app) {
			const status = data.app.status;
			if (status === 'running' || status === 'degraded' || status === 'updating') {
				connectContainerLogs();
			}
		}
	});

	async function refreshTasks() {
		try {
			await invalidateAll();
		} catch {}
	}

	async function refreshAppDomainState() {
		const latest = await api.getApp(data.projectId, data.appId);
		data.app = latest;
	}

	const taskCounts = $derived({
		running: (data.tasks ?? []).filter((t) => t.status === 'running').length,
		pending: (data.tasks ?? []).filter((t) => t.status === 'pending' || t.status === 'starting').length,
		failed: (data.tasks ?? []).filter((t) => t.status === 'failed' || t.status === 'rejected').length
	});

	async function handleDeploy() {
		if (!data.app) return;
		if (deployInProgress) {
			toast.info('A deployment is already in progress');
			return;
		}
		actionLoading = 'deploy';
		try {
			await api.deployApp(data.projectId, data.appId);
			toast.success('Deployment triggered');
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
			toast.error(e.message ?? 'Failed to trigger deployment');
		}
		actionLoading = '';
	}

	async function handleRestart() {
		if (!data.app) return;
		actionLoading = 'restart';
		try {
			await api.restartApp(data.projectId, data.appId);
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		}
		actionLoading = '';
	}

	async function handleStop() {
		if (!data.app) return;
		actionLoading = 'stop';
		try {
			await api.stopApp(data.projectId, data.appId);
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		}
		actionLoading = '';
	}

	async function handleStart() {
		if (!data.app) return;
		actionLoading = 'start';
		try {
			await api.startApp(data.projectId, data.appId);
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		}
		actionLoading = '';
	}

	async function handleScale() {
		if (!data.app) return;
		actionLoading = 'scale';
		try {
			await api.scaleApp(data.projectId, data.appId, scaleInput);
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		}
		actionLoading = '';
	}

	async function handleRollback() {
		if (!data.app || !confirm('Rollback to the previous version?')) return;
		actionLoading = 'rollback';
		try {
			await api.rollbackApp(data.projectId, data.appId);
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
		}
		actionLoading = '';
	}

	async function handleRollbackToDeployment(deploymentId: string) {
		if (!data.app) return;
		actionLoading = `rollback-${deploymentId}`;
		try {
			await api.rollbackToDeployment(data.projectId, data.appId, deploymentId);
			toast.success('Rollback started');
			await invalidateAll();
		} catch (e: any) {
			error = e.message;
			toast.error(e.message ?? 'Rollback failed');
		}
		actionLoading = '';
	}

	async function copyDeploymentLogs(logs: string) {
		try {
			await navigator.clipboard.writeText(logs);
			toast.success('Logs copied');
		} catch {
			toast.error('Failed to copy logs');
		}
	}

	async function handleDeleteApp() {
		if (!data.app) return;
		actionLoading = 'delete';
		try {
			await api.deleteApp(data.projectId, data.appId);
			goto(`/projects/${data.projectId}`);
		} catch (e: any) {
			error = e.message;
			showDeleteConfirm = false;
		}
		actionLoading = '';
	}

	async function handleExportAsTemplate() {
		if (!data.app) return;
		actionLoading = 'export';
		try {
			await api.exportAppAsTemplate(data.projectId, data.appId);
			error = '';
			actionLoading = '';
			if (confirm('Template created. Go to catalog?')) {
				goto('/catalog');
			}
		} catch (e: any) {
			error = e.message;
			actionLoading = '';
		}
	}

	function statusColor(status: string): string {
		switch (status) {
			case 'success':
			case 'running':
				return 'var(--color-success)';
			case 'building':
			case 'deploying':
			case 'pending':
			case 'starting':
				return 'var(--color-warning)';
			case 'failed':
			case 'stopped':
			case 'rejected':
				return 'var(--color-danger)';
			default:
				return 'var(--color-text-muted)';
		}
	}

	function statusBg(status: string): string {
		switch (status) {
			case 'success':
			case 'running':
				return 'rgba(34, 197, 94, 0.12)';
			case 'building':
			case 'deploying':
			case 'pending':
			case 'starting':
				return 'rgba(229, 160, 13, 0.12)';
			case 'failed':
			case 'stopped':
			case 'rejected':
				return 'rgba(239, 68, 68, 0.12)';
			default:
				return 'rgba(154, 145, 138, 0.12)';
		}
	}

	function formatBytes(bytes: number): string {
		if (bytes === 0) return '0 B';
		const k = 1024;
		const sizes = ['B', 'KB', 'MB', 'GB'];
		const i = Math.floor(Math.log(bytes) / Math.log(k));
		return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
	}
</script>

<svelte:head><title>{data.app?.name ?? 'App'} | Hive</title></svelte:head>

<div class="max-w-6xl mx-auto">
	{#if error}
		<div
			class="rounded-lg p-4 mb-4"
			style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);"
		>
			<p style="color: var(--color-danger);">{error}</p>
		</div>
	{/if}

	{#if data.app}
		<div class="mb-4">
			<a
				href="/projects/{data.projectId}"
				class="text-sm hover:underline"
				style="color: var(--color-text-muted);"
			>
				&larr; Back to project
			</a>
		</div>

		<div class="flex flex-col md:flex-row md:items-center justify-between gap-4 mb-6">
			<div class="min-w-0">
				<div class="flex items-center gap-3 flex-wrap">
					<h1 class="text-xl md:text-2xl font-bold">{data.app.name}</h1>
					<span
						class="px-2 py-0.5 rounded text-xs font-semibold uppercase"
						style="background-color: {statusBg(data.app.status)}; color: {statusColor(data.app.status)};"
					>
						{data.app.status}
					</span>
					{#if updateStatus?.update_available}
						<span class="px-2 py-0.5 rounded text-xs font-semibold" style="background: rgba(59,130,246,0.15); color: var(--color-info);">
							Update Available
						</span>
					{/if}
				</div>
			<div class="flex items-center gap-3 mt-1">
				<span class="text-sm" style="color: var(--color-text-muted);">{data.app.deploy_type}</span>
				{#if editingDomain}
					<div class="flex items-center gap-2 flex-wrap">
						<input
							type="text"
							bind:value={domainDraft}
							placeholder="app.example.com"
							class="px-2 py-1 rounded text-sm outline-none w-full sm:w-[200px]"
							style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
						/>
						<button
							onclick={async () => {
								actionLoading = 'domain';
								domainProvisioning = true;
								try {
									const updated = await api.updateApp(data.projectId, data.appId, { domain: domainDraft });
									data.app = updated;
									editingDomain = false;
									toast.success('Domain saved. Provisioning DNS and ingress routing...');
									setTimeout(async () => {
										try {
											await refreshAppDomainState();
										} catch {}
										domainProvisioning = false;
									}, 1500);
								} catch (e) {
									error = (e as Error).message;
									domainProvisioning = false;
								}
								actionLoading = '';
							}}
							disabled={actionLoading === 'domain'}
							class="text-xs px-2 py-1 rounded cursor-pointer font-medium"
							style="background-color: var(--color-primary); color: white;"
						>
							Save
						</button>
						<button onclick={() => editingDomain = false} class="text-xs px-2 py-1 cursor-pointer" style="color: var(--color-text-muted);">Cancel</button>
					</div>
			{:else if data.app.domain}
				<a
					href="https://{data.app.domain}"
					target="_blank"
					class="text-sm underline"
					style="color: var(--color-primary);"
				>
					{data.app.domain}
				</a>
				{#if data.app.dns_status === 'configured'}
					<span class="inline-flex items-center gap-1 text-xs" style="color: var(--color-success);">
						<span class="inline-block w-1.5 h-1.5 rounded-full" style="background-color: var(--color-success);"></span>
						DNS
					</span>
				{:else if data.app.dns_status === 'missing'}
					<span class="inline-flex items-center gap-1 text-xs" style="color: var(--color-warning);">
						<span class="inline-block w-1.5 h-1.5 rounded-full" style="background-color: var(--color-warning);"></span>
						No DNS record
					</span>
				{:else if data.app.has_dns_provider === false}
					<a href="/dns" class="inline-flex items-center gap-1 text-xs underline" style="color: var(--color-text-muted);">
						<span class="inline-block w-1.5 h-1.5 rounded-full" style="background-color: var(--color-text-muted);"></span>
						Configure DNS
					</a>
				{/if}
				<button
					onclick={() => { domainDraft = data.app.domain; editingDomain = true; }}
					class="text-xs px-2 py-0.5 rounded cursor-pointer"
					style="background-color: var(--color-bg); color: var(--color-text-muted); border: 1px solid var(--color-border);"
				>
					Edit
				</button>
				{:else}
					<button
						onclick={() => { domainDraft = ''; editingDomain = true; }}
						class="text-xs px-2 py-0.5 rounded cursor-pointer"
						style="background-color: var(--color-bg); color: var(--color-text-muted); border: 1px solid var(--color-border);"
					>
						Add Domain
					</button>
				{/if}
				{#if domainProvisioning}
					<span class="text-xs" style="color: var(--color-text-muted);">Provisioning DNS/routing...</span>
				{/if}
			</div>
			</div>
			<div class="flex gap-2 flex-wrap">
				{#if data.app.status === 'stopped'}
					<button
						onclick={handleStart}
						disabled={!!actionLoading}
						class="app-action-btn"
						style="background-color: var(--color-success); color: var(--color-bg);"
					>
						{actionLoading === 'start' ? '...' : 'Start'}
					</button>
				{:else}
					<button
						onclick={handleStop}
						disabled={!!actionLoading}
						class="app-action-btn"
						style="background-color: var(--color-text-muted); color: var(--color-bg);"
					>
						{actionLoading === 'stop' ? '...' : 'Stop'}
					</button>
				{/if}
				<button
					onclick={handleRestart}
					disabled={!!actionLoading}
					class="app-action-btn"
					style="background-color: var(--color-warning); color: var(--color-bg);"
				>
					{actionLoading === 'restart' ? '...' : 'Restart'}
				</button>
				<button
					onclick={handleDeploy}
					disabled={!!actionLoading || deployInProgress}
					class="app-action-btn"
					style="background-color: var(--color-primary); color: var(--color-bg);"
				>
					{actionLoading === 'deploy' ? '...' : deployInProgress ? 'Deploying...' : 'Deploy'}
				</button>
				<button
					onclick={handleRollback}
					disabled={!!actionLoading}
					class="app-action-btn"
					style="border: 1px solid var(--color-border); color: var(--color-text);"
				>
					{actionLoading === 'rollback' ? '...' : 'Rollback'}
				</button>
				<button
					onclick={handleExportAsTemplate}
					disabled={!!actionLoading}
					class="app-action-btn"
					style="border: 1px solid var(--color-border); color: var(--color-text);"
				>
					{actionLoading === 'export' ? '...' : 'Export'}
				</button>
				<button
					onclick={() => showDeleteConfirm = true}
					disabled={!!actionLoading}
					class="app-action-btn"
					style="border: 1px solid rgba(239,68,68,0.3); color: var(--color-danger);"
				>
					Delete
				</button>
			</div>
		</div>

		<!-- Tabs -->
		<div class="app-tabs mb-6 border-b" style="border-color: var(--color-border);">
			{#each ['overview', 'tasks', 'deployments', 'events', 'links', 'previews'] as tab}
				<button
					onclick={() => (activeTab = tab as any)}
					class="app-tab"
					style="border-color: {activeTab === tab ? 'var(--color-primary)' : 'transparent'}; color: {activeTab === tab ? 'var(--color-primary)' : 'var(--color-text-muted)'};"
				>
					{tab.charAt(0).toUpperCase() + tab.slice(1)}
				</button>
			{/each}
		</div>

		{#if activeTab === 'overview'}
			{#if data.app.status === 'failed'}
				{@const failedDeploy = (data.deployments ?? []).find((d) => d.status === 'failed')}
				<div class="rounded-lg p-4 mb-6" style="background: rgba(239, 68, 68, 0.08); border: 1px solid rgba(239, 68, 68, 0.3);">
					<div class="flex items-center justify-between gap-4 mb-2">
						<div class="flex items-center gap-2">
							<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--color-danger)" stroke-width="2"><path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
							<p class="text-sm font-semibold" style="color: var(--color-danger);">Deployment Failed</p>
						</div>
						<div class="flex gap-2">
							<button
								onclick={handleDeploy}
								disabled={!!actionLoading || deployInProgress}
								class="px-3 py-1.5 rounded text-xs font-semibold cursor-pointer"
								style="background: var(--color-primary); color: white;"
							>
								{actionLoading === 'deploy' ? 'Deploying...' : deployInProgress ? 'Deploying...' : 'Retry Deploy'}
							</button>
							<button
								onclick={() => showDeleteConfirm = true}
								class="px-3 py-1.5 rounded text-xs font-semibold cursor-pointer"
								style="border: 1px solid rgba(239,68,68,0.3); color: var(--color-danger);"
							>
								Remove
							</button>
						</div>
					</div>
					{#if failedDeploy?.logs}
						<pre class="mt-2 p-2 rounded text-xs overflow-auto font-mono" style="background-color: var(--color-bg); color: var(--color-text); max-height: 150px; border: 1px solid var(--color-border);">{failedDeploy.logs}</pre>
					{:else}
						<p class="text-xs mt-1" style="color: var(--color-text-muted);">No error logs available. Check the Deployments tab for details.</p>
					{/if}
				</div>
			{/if}

			{#if updateStatus?.update_available}
				<div class="rounded-lg p-4 mb-6 flex items-center justify-between gap-4" style="background: rgba(59,130,246,0.08); border: 1px solid rgba(59,130,246,0.25);">
					<div>
						<p class="text-sm font-semibold" style="color: var(--color-info);">Image Update Available</p>
						<p class="text-xs mt-1" style="color: var(--color-text-muted);">
							Current: <span class="font-mono">{updateStatus.current_image}</span>
							{#if updateStatus.latest_version}
								&rarr; <span class="font-mono" style="color: var(--color-success);">{updateStatus.latest_version}</span>
							{:else}
								(newer digest available)
							{/if}
						</p>
					</div>
					<button
						class="px-3 py-1.5 rounded text-xs font-semibold cursor-pointer"
						style="background: var(--color-info); color: white;"
						onclick={applyImageUpdate}
						disabled={updateLoading}
					>
						{updateLoading ? 'Updating...' : 'Update Now'}
					</button>
				</div>
			{/if}

			<div class="grid grid-cols-1 md:grid-cols-4 gap-4 mb-6">
				<div
					class="rounded-lg p-4"
					style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
				>
					<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">
						Image
					</p>
					<p class="text-sm font-mono truncate">{data.app.image || 'Built from source'}</p>
				</div>
				<div
					class="rounded-lg p-4"
					style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
				>
					<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">
						Port
					</p>
					<p class="text-sm">{data.app.port}</p>
				</div>
				<div
					class="rounded-lg p-4"
					style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
				>
					<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">
						Replicas
					</p>
					<p class="text-sm">{data.app.replicas}</p>
				</div>
				<div
					class="rounded-lg p-4"
					style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
				>
					<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">
						Resources
					</p>
					<p class="text-sm">
						{data.app.cpu_limit ? data.app.cpu_limit + ' CPU' : 'No limit'}
						{data.app.memory_limit ? ' / ' + formatBytes(data.app.memory_limit) : ''}
					</p>
				</div>
			</div>

			<!-- Task counts -->
			<div
				class="rounded-lg p-4 mb-6"
				style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
			>
				<h3 class="text-sm font-semibold mb-3">Task Status</h3>
				<div class="flex gap-6">
					<div>
						<span class="text-xs" style="color: var(--color-text-muted);">Running:</span>
						<span class="font-semibold ml-2" style="color: var(--color-success);"
							>{taskCounts.running}</span
						>
					</div>
					<div>
						<span class="text-xs" style="color: var(--color-text-muted);">Pending:</span>
						<span class="font-semibold ml-2" style="color: var(--color-warning);"
							>{taskCounts.pending}</span
						>
					</div>
					<div>
						<span class="text-xs" style="color: var(--color-text-muted);">Failed:</span>
						<span class="font-semibold ml-2" style="color: var(--color-danger);"
							>{taskCounts.failed}</span
						>
					</div>
				</div>
			</div>

			<!-- Access Links -->
			{#if data.app.domain || (data.ports ?? []).some((p) => p.published_port > 0)}
				<div
					class="rounded-lg p-4 mb-6"
					style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
				>
					<h3 class="text-sm font-semibold mb-3">Access Links</h3>
					<div class="space-y-2">
						{#if data.app.domain}
							<div class="flex items-center gap-2">
								<span class="text-xs uppercase tracking-wide w-16" style="color: var(--color-text-muted);">Domain</span>
								<a
									href="https://{data.app.domain}"
									target="_blank"
									rel="noopener noreferrer"
									class="text-sm underline"
									style="color: var(--color-primary);"
								>
									https://{data.app.domain}
								</a>
							</div>
						{/if}
						{#each data.ports ?? [] as p}
							{#if p.published_port > 0}
								{@const nodeIp = (data.nodes ?? [])[0]?.Status?.Addr || 'localhost'}
								<div class="flex items-center gap-2">
									<span class="text-xs uppercase tracking-wide w-16" style="color: var(--color-text-muted);">Port {p.published_port}</span>
									<a
										href="http://{nodeIp}:{p.published_port}"
										target="_blank"
										rel="noopener noreferrer"
										class="text-sm underline font-mono"
										style="color: var(--color-primary);"
									>
										http://{nodeIp}:{p.published_port}
									</a>
								</div>
							{/if}
						{/each}
					</div>
				</div>
			{/if}

			<!-- Port mappings -->
			{#if (data.ports ?? []).length > 0}
				<div
					class="rounded-lg p-4 mb-6"
					style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
				>
					<h3 class="text-sm font-semibold mb-3">Port Mappings</h3>
					<div class="overflow-x-auto">
						<table class="w-full text-sm">
							<thead>
								<tr class="text-left" style="color: var(--color-text-muted);">
									<th class="pb-2">Protocol</th>
									<th class="pb-2">Target</th>
									<th class="pb-2">Published</th>
									<th class="pb-2">Mode</th>
								</tr>
							</thead>
							<tbody>
								{#each data.ports ?? [] as p}
									<tr style="border-top: 1px solid var(--color-border);">
										<td class="py-2">{p.protocol}</td>
										<td class="py-2">{p.target_port}</td>
										<td class="py-2">{p.published_port}</td>
										<td class="py-2">{p.publish_mode}</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				</div>
			{/if}

			<!-- Scale control -->
			<div
				class="rounded-lg p-4 mb-6"
				style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
			>
				<h3 class="text-sm font-semibold mb-3">Scale</h3>
				<div class="flex items-center gap-3">
					<button
						onclick={() => {
							if (scaleInput > 0) scaleInput--;
						}}
						class="px-3 py-1 rounded"
						style="border: 1px solid var(--color-border); color: var(--color-text);"
					>
						-
					</button>
					<span class="text-lg font-mono w-8 text-center">{scaleInput}</span>
					<button
						onclick={() => scaleInput++}
						class="px-3 py-1 rounded"
						style="border: 1px solid var(--color-border); color: var(--color-text);"
					>
						+
					</button>
					<button
						onclick={handleScale}
						disabled={scaleInput === data.app.replicas || !!actionLoading}
						class="px-4 py-1 rounded text-sm font-medium ml-4"
						style="background-color: var(--color-primary); color: var(--color-bg); opacity: {scaleInput === data.app.replicas ? '0.5' : '1'};"
					>
						{actionLoading === 'scale' ? 'Scaling...' : 'Apply'}
					</button>
				</div>
			</div>

			<!-- Logs preview -->
			<div class="space-y-4">
				<div>
					<h3 class="text-sm font-semibold mb-2">Container Logs</h3>
					<div
						class="rounded-lg p-3 font-mono text-xs overflow-auto"
						style="background-color: var(--color-bg); color: var(--color-text); max-height: 200px; min-height: 80px; border: 1px solid var(--color-border);"
					>
						{#if logLines.length === 0}
							<p style="color: var(--color-text-muted);">Connecting to container logs...</p>
						{/if}
						{#each logLines.slice(-50) as line}
							<div class="whitespace-pre-wrap break-all">{line}</div>
						{/each}
					</div>
				</div>
			</div>
		{/if}

		{#if activeTab === 'tasks'}
			<div
				class="rounded-lg overflow-hidden"
				style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
			>
				<div class="flex items-center justify-between p-4 border-b" style="border-color: var(--color-border);">
					<h3 class="text-sm font-semibold">Container Tasks</h3>
					<button
						onclick={refreshTasks}
						class="px-3 py-1 rounded text-xs"
						style="background-color: var(--color-primary); color: var(--color-bg);"
					>
						Refresh
					</button>
				</div>
				<div class="overflow-x-auto">
					<table class="w-full text-sm">
						<thead>
							<tr class="text-left" style="color: var(--color-text-muted); background-color: var(--color-bg);">
								<th class="px-4 py-3">Status</th>
								<th class="px-4 py-3">Slot</th>
								<th class="px-4 py-3">Node</th>
								<th class="px-4 py-3">Image</th>
								<th class="px-4 py-3">Created</th>
								<th class="px-4 py-3">Message</th>
							</tr>
						</thead>
						<tbody>
							{#each data.tasks ?? [] as t}
								<tr style="border-top: 1px solid var(--color-border);">
									<td class="px-4 py-3">
										<span
											class="px-2 py-0.5 rounded text-xs font-medium"
											style="background-color: {statusBg(t.status)}; color: {statusColor(t.status)};"
										>
											{t.status}
										</span>
									</td>
									<td class="px-4 py-3">{t.slot}</td>
									<td class="px-4 py-3 font-mono text-xs">{t.node_id.slice(0, 12)}...</td>
									<td class="px-4 py-3 font-mono text-xs truncate max-w-[200px]">{t.image || '-'}</td>
									<td class="px-4 py-3 text-xs" style="color: var(--color-text-muted);">
										{new Date(t.created_at).toLocaleString()}
									</td>
									<td class="px-4 py-3 text-xs" style="color: var(--color-text-muted); max-w-[200px] truncate">
										{t.message || '-'}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
					{#if (data.tasks ?? []).length === 0}
						<p class="p-6 text-sm" style="color: var(--color-text-muted);">No tasks found for this service.</p>
					{/if}
				</div>
			</div>
		{/if}

		{#if activeTab === 'deployments'}
			<div class="space-y-2">
				{#each data.deployments ?? [] as deploy}
					<div
						class="rounded-lg p-4"
						style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
					>
						<div class="flex items-center justify-between mb-2">
							<div class="flex items-center gap-3">
								<span
									class="inline-block w-2 h-2 rounded-full"
									style="background-color: {statusColor(deploy.status)};"
								></span>
								<span class="text-sm font-semibold">{deploy.status}</span>
								<span class="text-xs font-mono" style="color: var(--color-text-muted);"
									>{deploy.id.slice(0, 8)}</span
								>
								{#if deploy.commit_sha}
									<span class="text-xs font-mono" style="color: var(--color-text-muted);"
										>@ {deploy.commit_sha.slice(0, 7)}</span
									>
								{/if}
							</div>
							<div class="text-xs" style="color: var(--color-text-muted);">
								{new Date(deploy.started_at).toLocaleString()}
								{#if deploy.finished_at}
									&mdash; {new Date(deploy.finished_at).toLocaleString()}
								{/if}
							</div>
						</div>
						<div class="flex items-center gap-2 mb-2">
							<button
								onclick={() => handleRollbackToDeployment(deploy.id)}
								disabled={!!actionLoading}
								class="px-2 py-1 rounded text-xs font-semibold cursor-pointer"
								style="border: 1px solid var(--color-border); color: var(--color-text);"
							>
								{actionLoading === `rollback-${deploy.id}` ? 'Rolling back...' : 'Rollback to this'}
							</button>
							{#if deploy.logs}
								<button
									onclick={() => copyDeploymentLogs(deploy.logs)}
									class="px-2 py-1 rounded text-xs font-semibold cursor-pointer"
									style="border: 1px solid var(--color-border); color: var(--color-text-muted);"
								>
									Copy logs
								</button>
							{/if}
						</div>
						{#if deploy.logs}
							<details class="mt-2">
								<summary class="text-xs cursor-pointer" style="color: var(--color-primary);">
									View logs
								</summary>
								<pre
									class="mt-2 p-2 rounded text-xs overflow-auto font-mono"
									style="background-color: var(--color-bg); color: var(--color-text); max-height: 200px; border: 1px solid var(--color-border);"
								>
									{deploy.logs}
								</pre>
							</details>
						{/if}
					</div>
				{/each}
				{#if (data.deployments ?? []).length === 0}
					<p class="text-sm py-4" style="color: var(--color-text-muted);">No deployments yet</p>
				{/if}
			</div>
		{/if}

		{#if activeTab === 'events'}
			<div
				class="rounded-lg p-4"
				style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
			>
				<div class="flex items-center justify-between mb-4">
					<h3 class="text-sm font-semibold">Swarm Events</h3>
					<button
						onclick={refreshTasks}
						class="px-3 py-1 rounded text-xs"
						style="background-color: var(--color-primary); color: var(--color-bg);"
					>
						Refresh
					</button>
				</div>
				<div class="space-y-2">
					{#each data.events ?? [] as evt}
						<div
							class="flex gap-4 py-2 border-b"
							style="border-color: var(--color-border);"
						>
							<span class="text-xs shrink-0" style="color: var(--color-text-muted);">
								{new Date(evt.time).toLocaleString()}
							</span>
							<span
								class="px-2 py-0.5 rounded text-xs font-medium shrink-0"
								style="background-color: var(--color-bg); color: var(--color-text);"
							>
								{evt.action}
							</span>
							<span class="text-sm truncate" style="color: var(--color-text);">{evt.message}</span>
						</div>
					{/each}
					{#if (data.events ?? []).length === 0}
						<p class="text-sm py-4" style="color: var(--color-text-muted);">
							No events in the last hour.
						</p>
					{/if}
				</div>
			</div>
		{/if}

		{#if activeTab === 'links'}
			<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<h3 class="text-sm font-semibold mb-4">Service Links</h3>
				{#if (data.serviceLinks ?? []).length === 0}
					<p class="text-sm" style="color: var(--color-text-muted);">No service links configured. Links connect this app to other services or databases via injected environment variables.</p>
				{:else}
					<div class="space-y-2">
						{#each data.serviceLinks ?? [] as link}
							<div class="flex items-center justify-between py-2 border-b" style="border-color: var(--color-border);">
								<div class="text-sm">
									<span class="font-mono text-xs px-1.5 py-0.5 rounded" style="background-color: var(--color-bg);">{link.env_prefix}</span>
									<span class="mx-2" style="color: var(--color-text-muted);">&rarr;</span>
									<span>{link.target_app_id || link.target_database_id}</span>
								</div>
								<button onclick={async () => {
									try {
										await api.deleteServiceLink(data.projectId, data.appId, link.id);
										await invalidateAll();
									} catch (e) { error = (e as Error).message; }
								}}
									class="text-xs px-2 py-1 rounded" style="color: var(--color-danger);">
									Remove
								</button>
							</div>
						{/each}
					</div>
				{/if}
			</div>
		{/if}

		{#if activeTab === 'previews'}
			<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<h3 class="text-sm font-semibold mb-4">Preview Deployments</h3>
				{#if (data.previews ?? []).length === 0}
					<p class="text-sm" style="color: var(--color-text-muted);">No preview deployments. Enable preview environments and push a PR to generate one.</p>
				{:else}
					<div class="space-y-2">
						{#each data.previews ?? [] as preview}
							<div class="flex items-center justify-between py-3 border-b" style="border-color: var(--color-border);">
								<div>
									<div class="flex items-center gap-2">
										<span class="inline-block w-2 h-2 rounded-full" style="background-color: {statusColor(preview.status)};"></span>
										<span class="text-sm font-medium">{preview.branch}</span>
										{#if preview.pr_number}
											<span class="text-xs" style="color: var(--color-text-muted);">PR #{preview.pr_number}</span>
										{/if}
									</div>
									{#if preview.domain}
										<a href="https://{preview.domain}" target="_blank" class="text-xs underline mt-1 inline-block" style="color: var(--color-primary);">
											{preview.domain}
										</a>
									{/if}
								</div>
								<div class="flex items-center gap-3">
									<span class="text-xs" style="color: var(--color-text-muted);">{new Date(preview.created_at).toLocaleDateString()}</span>
									<button onclick={async () => {
										try {
											await api.deletePreview(data.projectId, data.appId, preview.id);
											await invalidateAll();
										} catch (e) { error = (e as Error).message; }
									}}
										class="text-xs px-2 py-1 rounded" style="color: var(--color-danger);">
										Destroy
									</button>
								</div>
							</div>
						{/each}
					</div>
				{/if}
			</div>
		{/if}
	{/if}
</div>

{#if showDeleteConfirm}
	<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
	<div class="modal-backdrop" onclick={() => showDeleteConfirm = false}>
		<div class="modal-content" onclick={(e) => e.stopPropagation()}>
			<div class="modal-header">
				<h3 class="text-lg font-semibold">Delete App</h3>
			</div>
			<div class="modal-body">
				<p class="text-sm mb-4">Are you sure you want to delete <strong>{data.app?.name}</strong>? This will remove the Docker service and all associated data. This action cannot be undone.</p>
				<div class="flex gap-2 justify-end flex-wrap">
					<button onclick={() => showDeleteConfirm = false} class="btn btn-ghost btn-sm">Cancel</button>
					<button onclick={handleDeleteApp} disabled={actionLoading === 'delete'} class="btn btn-danger-filled btn-sm">
						{actionLoading === 'delete' ? 'Deleting...' : 'Delete App'}
					</button>
				</div>
			</div>
		</div>
	</div>
{/if}

<style>
	.app-action-btn {
		padding: 0.375rem 0.75rem;
		border-radius: var(--radius-md);
		font-size: var(--text-sm);
		font-weight: 500;
		cursor: pointer;
		white-space: nowrap;
		transition: all var(--transition-base);
	}
	.app-action-btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.app-tabs {
		display: flex;
		gap: 0.25rem;
		overflow-x: auto;
		white-space: nowrap;
		scrollbar-width: none;
		-webkit-overflow-scrolling: touch;
	}
	.app-tabs::-webkit-scrollbar {
		display: none;
	}

	.app-tab {
		padding: 0.5rem 1rem;
		font-size: var(--text-sm);
		font-weight: 500;
		border-bottom: 2px solid transparent;
		transition: color var(--transition-fast), border-color var(--transition-fast);
		white-space: nowrap;
		flex-shrink: 0;
		cursor: pointer;
	}

	@media (max-width: 768px) {
		.app-action-btn {
			min-height: 40px;
			padding: 0.5rem 0.875rem;
		}
		.app-tab {
			min-height: 44px;
			padding: 0.625rem 1rem;
		}
	}
</style>
