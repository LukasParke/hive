<script lang="ts">
	import { page } from '$app/stores';
	import {
		api,
		type App,
		type Deployment,
		type TaskInfo,
		type ServiceEvent,
		type PortMapping,
		type ServiceLink,
		type PreviewDeployment
	} from '$lib/api';
	import { onMount, onDestroy } from 'svelte';

	let app = $state<App | null>(null);
	let deployments = $state<Deployment[]>([]);
	let tasks = $state<TaskInfo[]>([]);
	let events = $state<ServiceEvent[]>([]);
	let ports = $state<PortMapping[]>([]);
	let serviceLinks = $state<ServiceLink[]>([]);
	let previews = $state<PreviewDeployment[]>([]);
	let error = $state('');
	let loading = $state(true);
	let activeTab = $state<'overview' | 'tasks' | 'deployments' | 'events' | 'links' | 'previews'>('overview');
	let logLines = $state<string[]>([]);
	let buildLines = $state<string[]>([]);
	let scaleInput = $state(1);
	let actionLoading = $state('');

	const projectId = $derived($page.params.id ?? '');
	const appId = $derived(($page.params as Record<string, string>).appId ?? '');

	let logSocket: WebSocket | null = null;
	let buildSocket: WebSocket | null = null;

	onMount(async () => {
		await loadAll();
	});

	onDestroy(() => {
		logSocket?.close();
		buildSocket?.close();
	});

	async function loadAll() {
		if (!projectId || !appId) return;
		try {
			loading = true;
			[app, deployments, tasks, events, ports, serviceLinks, previews] = await Promise.all([
				api.getApp(projectId, appId),
				api.listDeployments(projectId, appId),
				api.getAppTasks(projectId, appId),
				api.getAppEvents(projectId, appId),
				api.getAppPorts(projectId, appId),
				api.listServiceLinks(projectId, appId).catch(() => []),
				api.listPreviews(projectId, appId).catch(() => [])
			]);
			if (app) scaleInput = app.replicas;
			error = '';
		} catch (e: any) {
			error = e.message;
		} finally {
			loading = false;
		}
	}

	async function loadApp() {
		if (!projectId || !appId) return;
		try {
			app = await api.getApp(projectId, appId);
			deployments = await api.listDeployments(projectId, appId);
			tasks = await api.getAppTasks(projectId, appId);
			ports = await api.getAppPorts(projectId, appId);
			if (app) scaleInput = app.replicas;
			error = '';
		} catch (e: any) {
			error = e.message;
		}
	}

	function connectContainerLogs() {
		if (!app) return;
		logSocket?.close();
		logLines = [];
		const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
		const url = `${proto}//${location.host}/api/v1/projects/${projectId}/apps/${appId}/container-logs?name=${encodeURIComponent(app.name)}&tail=200`;
		logSocket = new WebSocket(url);
		logSocket.onmessage = (e) => {
			logLines = [...logLines.slice(-999), e.data];
		};
	}

	function connectBuildLogs() {
		if (!app) return;
		buildSocket?.close();
		buildLines = [];
		const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
		const url = `${proto}//${location.host}/api/v1/projects/${projectId}/apps/${appId}/logs`;
		buildSocket = new WebSocket(url);
		buildSocket.onmessage = (e) => {
			try {
				const data = JSON.parse(e.data);
				buildLines = [...buildLines.slice(-999), data.message || e.data];
			} catch {
				buildLines = [...buildLines.slice(-999), e.data];
			}
		};
	}

	$effect(() => {
		if (activeTab === 'overview' && app) {
			connectContainerLogs();
			connectBuildLogs();
		}
	});

	async function refreshTasks() {
		if (!projectId || !appId) return;
		try {
			tasks = await api.getAppTasks(projectId, appId);
			events = await api.getAppEvents(projectId, appId);
		} catch {}
	}

	const taskCounts = $derived({
		running: tasks.filter((t) => t.status === 'running').length,
		pending: tasks.filter((t) => t.status === 'pending' || t.status === 'starting').length,
		failed: tasks.filter((t) => t.status === 'failed' || t.status === 'rejected').length
	});

	async function handleDeploy() {
		if (!app) return;
		actionLoading = 'deploy';
		try {
			await api.deployApp(projectId, appId);
			await loadAll();
		} catch (e: any) {
			error = e.message;
		}
		actionLoading = '';
	}

	async function handleRestart() {
		if (!app) return;
		actionLoading = 'restart';
		try {
			await api.restartApp(projectId, appId);
			await loadApp();
		} catch (e: any) {
			error = e.message;
		}
		actionLoading = '';
	}

	async function handleStop() {
		if (!app) return;
		actionLoading = 'stop';
		try {
			await api.stopApp(projectId, appId);
			await loadApp();
		} catch (e: any) {
			error = e.message;
		}
		actionLoading = '';
	}

	async function handleStart() {
		if (!app) return;
		actionLoading = 'start';
		try {
			await api.startApp(projectId, appId);
			await loadApp();
		} catch (e: any) {
			error = e.message;
		}
		actionLoading = '';
	}

	async function handleScale() {
		if (!app) return;
		actionLoading = 'scale';
		try {
			await api.scaleApp(projectId, appId, scaleInput);
			await loadApp();
		} catch (e: any) {
			error = e.message;
		}
		actionLoading = '';
	}

	async function handleRollback() {
		if (!app || !confirm('Rollback to the previous version?')) return;
		actionLoading = 'rollback';
		try {
			await api.rollbackApp(projectId, appId);
			await loadApp();
		} catch (e: any) {
			error = e.message;
		}
		actionLoading = '';
	}

	async function handleExportAsTemplate() {
		if (!app) return;
		actionLoading = 'export';
		try {
			await api.exportAppAsTemplate(projectId, appId);
			error = '';
			actionLoading = '';
			// Could navigate to catalog or show toast
			if (confirm('Template created. Go to catalog?')) {
				window.location.href = '/catalog';
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

	function formatBytes(bytes: number): string {
		if (bytes === 0) return '0 B';
		const k = 1024;
		const sizes = ['B', 'KB', 'MB', 'GB'];
		const i = Math.floor(Math.log(bytes) / Math.log(k));
		return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
	}
</script>

<div class="max-w-6xl mx-auto p-6">
	{#if loading}
		<div class="flex items-center justify-center py-20">
			<div
				class="animate-spin rounded-full h-8 w-8 border-b-2"
				style="border-color: var(--color-primary);"
			></div>
		</div>
	{:else if error}
		<div
			class="rounded-lg p-4 mb-4"
			style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);"
		>
			<p style="color: var(--color-danger);">{error}</p>
		</div>
	{/if}

	{#if app}
		<div class="mb-4">
			<a
				href="/projects/{projectId}"
				class="text-sm hover:underline"
				style="color: var(--color-text-muted);"
			>
				&larr; Back to project
			</a>
		</div>

		<div class="flex items-center justify-between mb-6">
			<div>
				<div class="flex items-center gap-3">
					<h1 class="text-2xl font-bold">{app.name}</h1>
					<span
						class="px-2 py-0.5 rounded text-xs font-semibold uppercase"
						style="background-color: {statusColor(app.status)}20; color: {statusColor(app.status)};"
					>
						{app.status}
					</span>
				</div>
				<div class="flex items-center gap-3 mt-1">
					<span class="text-sm" style="color: var(--color-text-muted);">{app.deploy_type}</span>
					{#if app.domain}
						<a
							href="https://{app.domain}"
							target="_blank"
							class="text-sm underline"
							style="color: var(--color-primary);"
						>
							{app.domain}
						</a>
					{/if}
				</div>
			</div>
			<div class="flex gap-2">
				{#if app.status === 'stopped'}
					<button
						onclick={handleStart}
						disabled={!!actionLoading}
						class="px-3 py-1.5 rounded text-sm font-medium"
						style="background-color: var(--color-success); color: var(--color-bg);"
					>
						{actionLoading === 'start' ? '...' : 'Start'}
					</button>
				{:else}
					<button
						onclick={handleStop}
						disabled={!!actionLoading}
						class="px-3 py-1.5 rounded text-sm font-medium"
						style="background-color: var(--color-text-muted); color: var(--color-bg);"
					>
						{actionLoading === 'stop' ? '...' : 'Stop'}
					</button>
				{/if}
				<button
					onclick={handleRestart}
					disabled={!!actionLoading}
					class="px-3 py-1.5 rounded text-sm font-medium"
					style="background-color: var(--color-warning); color: var(--color-bg);"
				>
					{actionLoading === 'restart' ? '...' : 'Restart'}
				</button>
				<button
					onclick={handleDeploy}
					disabled={!!actionLoading}
					class="px-3 py-1.5 rounded text-sm font-medium"
					style="background-color: var(--color-primary); color: var(--color-bg);"
				>
					{actionLoading === 'deploy' ? '...' : 'Deploy'}
				</button>
				<button
					onclick={handleRollback}
					disabled={!!actionLoading}
					class="px-3 py-1.5 rounded text-sm font-medium"
					style="border: 1px solid var(--color-border); color: var(--color-text);"
				>
					{actionLoading === 'rollback' ? '...' : 'Rollback'}
				</button>
				<button
					onclick={handleExportAsTemplate}
					disabled={!!actionLoading}
					class="px-3 py-1.5 rounded text-sm font-medium"
					style="border: 1px solid var(--color-border); color: var(--color-text);"
				>
					{actionLoading === 'export' ? '...' : 'Export as Template'}
				</button>
			</div>
		</div>

		<!-- Tabs -->
		<div class="flex gap-1 mb-6 border-b" style="border-color: var(--color-border);">
			{#each ['overview', 'tasks', 'deployments', 'events', 'links', 'previews'] as tab}
				<button
					onclick={() => (activeTab = tab as any)}
					class="px-4 py-2 text-sm font-medium border-b-2 transition-colors"
					style="border-color: {activeTab === tab ? 'var(--color-primary)' : 'transparent'}; color: {activeTab === tab ? 'var(--color-primary)' : 'var(--color-text-muted)'};"
				>
					{tab.charAt(0).toUpperCase() + tab.slice(1)}
				</button>
			{/each}
		</div>

		{#if activeTab === 'overview'}
			<div class="grid grid-cols-1 md:grid-cols-4 gap-4 mb-6">
				<div
					class="rounded-lg p-4"
					style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
				>
					<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">
						Image
					</p>
					<p class="text-sm font-mono truncate">{app.image || 'Built from source'}</p>
				</div>
				<div
					class="rounded-lg p-4"
					style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
				>
					<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">
						Port
					</p>
					<p class="text-sm">{app.port}</p>
				</div>
				<div
					class="rounded-lg p-4"
					style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
				>
					<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">
						Replicas
					</p>
					<p class="text-sm">{app.replicas}</p>
				</div>
				<div
					class="rounded-lg p-4"
					style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
				>
					<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">
						Resources
					</p>
					<p class="text-sm">
						{app.cpu_limit ? app.cpu_limit + ' CPU' : 'No limit'}
						{app.memory_limit ? ' / ' + formatBytes(app.memory_limit) : ''}
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

			<!-- Port mappings -->
			{#if ports.length > 0}
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
								{#each ports as p}
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
						disabled={scaleInput === app.replicas || !!actionLoading}
						class="px-4 py-1 rounded text-sm font-medium ml-4"
						style="background-color: var(--color-primary); color: var(--color-bg); opacity: {scaleInput === app.replicas ? '0.5' : '1'};"
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
							{#each tasks as t}
								<tr style="border-top: 1px solid var(--color-border);">
									<td class="px-4 py-3">
										<span
											class="px-2 py-0.5 rounded text-xs font-medium"
											style="background-color: {statusColor(t.status)}20; color: {statusColor(t.status)};"
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
					{#if tasks.length === 0}
						<p class="p-6 text-sm" style="color: var(--color-text-muted);">No tasks found for this service.</p>
					{/if}
				</div>
			</div>
		{/if}

		{#if activeTab === 'deployments'}
			<div class="space-y-2">
				{#each deployments as deploy}
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
				{#if deployments.length === 0}
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
					{#each events as evt}
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
					{#if events.length === 0}
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
				{#if serviceLinks.length === 0}
					<p class="text-sm" style="color: var(--color-text-muted);">No service links configured. Links connect this app to other services or databases via injected environment variables.</p>
				{:else}
					<div class="space-y-2">
						{#each serviceLinks as link}
							<div class="flex items-center justify-between py-2 border-b" style="border-color: var(--color-border);">
								<div class="text-sm">
									<span class="font-mono text-xs px-1.5 py-0.5 rounded" style="background-color: var(--color-bg);">{link.env_prefix}</span>
									<span class="mx-2" style="color: var(--color-text-muted);">&rarr;</span>
									<span>{link.target_app_id || link.target_database_id}</span>
								</div>
								<button onclick={async () => {
									try {
										await api.deleteServiceLink(projectId, appId, link.id);
										serviceLinks = serviceLinks.filter(l => l.id !== link.id);
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
				{#if previews.length === 0}
					<p class="text-sm" style="color: var(--color-text-muted);">No preview deployments. Enable preview environments and push a PR to generate one.</p>
				{:else}
					<div class="space-y-2">
						{#each previews as preview}
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
											await api.deletePreview(projectId, appId, preview.id);
											previews = previews.filter(p => p.id !== preview.id);
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
