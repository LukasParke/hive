<script lang="ts">
	import { page } from '$app/stores';
	import { api, type App, type Deployment } from '$lib/api';
	import { onMount, onDestroy } from 'svelte';

	let app = $state<App | null>(null);
	let deployments = $state<Deployment[]>([]);
	let error = $state('');
	let loading = $state(true);
	let activeTab = $state<'overview' | 'logs' | 'deployments' | 'settings'>('overview');
	let logLines = $state<string[]>([]);
	let buildLines = $state<string[]>([]);
	let scaleInput = $state(1);
	let actionLoading = $state('');

	const appId = $derived($page.params.id ?? '');
	const projectId = $derived($page.url.searchParams.get('project') ?? '');

	let logSocket: WebSocket | null = null;
	let buildSocket: WebSocket | null = null;

	onMount(async () => {
		if (!projectId) {
			error = 'Missing project context. Navigate from a project page.';
			loading = false;
			return;
		}
		await loadApp();
	});

	onDestroy(() => {
		logSocket?.close();
		buildSocket?.close();
	});

	async function loadApp() {
		try {
			loading = true;
			app = await api.getApp(projectId, appId);
			scaleInput = app.replicas;
			deployments = await api.listDeployments(projectId, appId);
			error = '';
		} catch (e: any) {
			error = e.message;
		} finally {
			loading = false;
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
		if (activeTab === 'logs' && app) {
			connectContainerLogs();
			connectBuildLogs();
		}
	});

	async function handleDeploy() {
		if (!app) return;
		actionLoading = 'deploy';
		try {
			await api.deployApp(projectId, appId);
			await loadApp();
		} catch (e: any) { error = e.message; }
		actionLoading = '';
	}

	async function handleRestart() {
		if (!app) return;
		actionLoading = 'restart';
		try {
			await api.restartApp(projectId, appId);
			await loadApp();
		} catch (e: any) { error = e.message; }
		actionLoading = '';
	}

	async function handleStop() {
		if (!app) return;
		actionLoading = 'stop';
		try {
			await api.stopApp(projectId, appId);
			await loadApp();
		} catch (e: any) { error = e.message; }
		actionLoading = '';
	}

	async function handleStart() {
		if (!app) return;
		actionLoading = 'start';
		try {
			await api.startApp(projectId, appId);
			await loadApp();
		} catch (e: any) { error = e.message; }
		actionLoading = '';
	}

	async function handleScale() {
		if (!app) return;
		actionLoading = 'scale';
		try {
			await api.scaleApp(projectId, appId, scaleInput);
			await loadApp();
		} catch (e: any) { error = e.message; }
		actionLoading = '';
	}

	async function handleRollback() {
		if (!app || !confirm('Rollback to the previous version?')) return;
		actionLoading = 'rollback';
		try {
			await api.rollbackApp(projectId, appId);
			await loadApp();
		} catch (e: any) { error = e.message; }
		actionLoading = '';
	}

	function statusColor(status: string): string {
		switch (status) {
			case 'success':
			case 'running': return '#22c55e';
			case 'building':
			case 'deploying': return '#f59e0b';
			case 'failed': return '#ef4444';
			case 'stopped': return '#6b7280';
			default: return '#94a3b8';
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
			<div class="animate-spin rounded-full h-8 w-8 border-b-2" style="border-color: var(--color-primary);"></div>
		</div>
	{:else if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid #ef4444;">
			<p style="color: #ef4444;">{error}</p>
		</div>
	{/if}

	{#if app}
		<div class="flex items-center justify-between mb-6">
			<div>
				<div class="flex items-center gap-3">
					<h1 class="text-2xl font-bold">{app.name}</h1>
					<span class="px-2 py-0.5 rounded text-xs font-semibold uppercase"
						style="background-color: {statusColor(app.status)}20; color: {statusColor(app.status)};">{app.status}</span>
				</div>
				<div class="flex items-center gap-3 mt-1">
					<span class="text-sm" style="color: var(--color-text-muted);">{app.deploy_type}</span>
					{#if app.domain}
						<a href="https://{app.domain}" target="_blank" class="text-sm underline" style="color: var(--color-primary);">{app.domain}</a>
					{/if}
				</div>
			</div>
			<div class="flex gap-2">
				{#if app.status === 'stopped'}
					<button onclick={handleStart} disabled={!!actionLoading}
						class="px-3 py-1.5 rounded text-sm font-medium text-white" style="background-color: #22c55e;">
						{actionLoading === 'start' ? '...' : 'Start'}
					</button>
				{:else}
					<button onclick={handleStop} disabled={!!actionLoading}
						class="px-3 py-1.5 rounded text-sm font-medium text-white" style="background-color: #6b7280;">
						{actionLoading === 'stop' ? '...' : 'Stop'}
					</button>
				{/if}
				<button onclick={handleRestart} disabled={!!actionLoading}
					class="px-3 py-1.5 rounded text-sm font-medium text-white" style="background-color: #f59e0b;">
					{actionLoading === 'restart' ? '...' : 'Restart'}
				</button>
				<button onclick={handleDeploy} disabled={!!actionLoading}
					class="px-3 py-1.5 rounded text-sm font-medium text-white" style="background-color: var(--color-primary);">
					{actionLoading === 'deploy' ? '...' : 'Deploy'}
				</button>
				<button onclick={handleRollback} disabled={!!actionLoading}
					class="px-3 py-1.5 rounded text-sm font-medium" style="border: 1px solid var(--color-border);">
					{actionLoading === 'rollback' ? '...' : 'Rollback'}
				</button>
			</div>
		</div>

		<!-- Tabs -->
		<div class="flex gap-1 mb-6 border-b" style="border-color: var(--color-border);">
			{#each ['overview', 'logs', 'deployments', 'settings'] as tab}
				<button
					onclick={() => activeTab = tab as any}
					class="px-4 py-2 text-sm font-medium border-b-2 transition-colors"
					style="border-color: {activeTab === tab ? 'var(--color-primary)' : 'transparent'}; color: {activeTab === tab ? 'var(--color-primary)' : 'var(--color-text-muted)'};">
					{tab.charAt(0).toUpperCase() + tab.slice(1)}
				</button>
			{/each}
		</div>

		{#if activeTab === 'overview'}
			<div class="grid grid-cols-1 md:grid-cols-4 gap-4 mb-8">
				<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
					<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">Image</p>
					<p class="text-sm font-mono truncate">{app.image || 'Built from source'}</p>
				</div>
				<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
					<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">Port</p>
					<p class="text-sm">{app.port}</p>
				</div>
				<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
					<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">Replicas</p>
					<p class="text-sm">{app.replicas}</p>
				</div>
				<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
					<p class="text-xs uppercase tracking-wide mb-1" style="color: var(--color-text-muted);">Resources</p>
					<p class="text-sm">
						{app.cpu_limit ? app.cpu_limit + ' CPU' : 'No limit'}
						{app.memory_limit ? ' / ' + formatBytes(app.memory_limit) : ''}
					</p>
				</div>
			</div>

			<!-- Scale control -->
			<div class="rounded-lg p-4 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<h3 class="text-sm font-semibold mb-3">Scale</h3>
				<div class="flex items-center gap-3">
					<button onclick={() => { if (scaleInput > 0) scaleInput-- }} class="px-3 py-1 rounded" style="border: 1px solid var(--color-border);">-</button>
					<span class="text-lg font-mono w-8 text-center">{scaleInput}</span>
					<button onclick={() => scaleInput++} class="px-3 py-1 rounded" style="border: 1px solid var(--color-border);">+</button>
					<button onclick={handleScale} disabled={scaleInput === app.replicas || !!actionLoading}
						class="px-4 py-1 rounded text-sm font-medium text-white ml-4"
						style="background-color: var(--color-primary); opacity: {scaleInput === app.replicas ? '0.5' : '1'};">
						{actionLoading === 'scale' ? 'Scaling...' : 'Apply'}
					</button>
				</div>
			</div>

			<!-- Recent deployments -->
			<h3 class="text-sm font-semibold mb-3">Recent Deployments</h3>
			<div class="space-y-2">
				{#each deployments.slice(0, 5) as deploy}
					<div class="rounded-lg p-3 flex items-center justify-between" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
						<div class="flex items-center gap-3">
							<span class="inline-block w-2 h-2 rounded-full" style="background-color: {statusColor(deploy.status)};"></span>
							<span class="text-sm font-medium">{deploy.status}</span>
							{#if deploy.commit_sha}
								<span class="text-xs font-mono" style="color: var(--color-text-muted);">{deploy.commit_sha.slice(0, 7)}</span>
							{/if}
						</div>
						<span class="text-xs" style="color: var(--color-text-muted);">{new Date(deploy.started_at).toLocaleString()}</span>
					</div>
				{/each}
				{#if deployments.length === 0}
					<p class="text-sm" style="color: var(--color-text-muted);">No deployments yet</p>
				{/if}
			</div>
		{/if}

		{#if activeTab === 'logs'}
			<div class="space-y-4">
				<div>
					<h3 class="text-sm font-semibold mb-2">Container Logs</h3>
					<div class="rounded-lg p-3 font-mono text-xs overflow-auto" style="background-color: #0d1117; color: #c9d1d9; max-height: 400px; min-height: 200px;">
						{#if logLines.length === 0}
							<p style="color: #484f58;">Connecting to container logs...</p>
						{/if}
						{#each logLines as line}
							<div class="whitespace-pre-wrap break-all">{line}</div>
						{/each}
					</div>
				</div>
				<div>
					<h3 class="text-sm font-semibold mb-2">Build Progress</h3>
					<div class="rounded-lg p-3 font-mono text-xs overflow-auto" style="background-color: #0d1117; color: #c9d1d9; max-height: 300px; min-height: 100px;">
						{#if buildLines.length === 0}
							<p style="color: #484f58;">No active build. Deploy to see build output.</p>
						{/if}
						{#each buildLines as line}
							<div class="whitespace-pre-wrap break-all">{line}</div>
						{/each}
					</div>
				</div>
			</div>
		{/if}

		{#if activeTab === 'deployments'}
			<div class="space-y-2">
				{#each deployments as deploy}
					<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
						<div class="flex items-center justify-between mb-2">
							<div class="flex items-center gap-3">
								<span class="inline-block w-2 h-2 rounded-full" style="background-color: {statusColor(deploy.status)};"></span>
								<span class="text-sm font-semibold">{deploy.status}</span>
								<span class="text-xs font-mono" style="color: var(--color-text-muted);">{deploy.id.slice(0, 8)}</span>
								{#if deploy.commit_sha}
									<span class="text-xs font-mono" style="color: var(--color-text-muted);">@ {deploy.commit_sha.slice(0, 7)}</span>
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
								<summary class="text-xs cursor-pointer" style="color: var(--color-primary);">View logs</summary>
								<pre class="mt-2 p-2 rounded text-xs overflow-auto font-mono" style="background-color: #0d1117; color: #c9d1d9; max-height: 200px;">{deploy.logs}</pre>
							</details>
						{/if}
					</div>
				{/each}
				{#if deployments.length === 0}
					<p class="text-sm" style="color: var(--color-text-muted);">No deployments yet</p>
				{/if}
			</div>
		{/if}

		{#if activeTab === 'settings'}
			<div class="space-y-6">
				<ResourceSettings {app} {projectId} onUpdate={loadApp} />
				<HealthCheckSettings {app} {projectId} onUpdate={loadApp} />
			</div>
		{/if}
	{/if}
</div>

{#snippet ResourceSettings(props: { app: App; projectId: string; onUpdate: () => Promise<void> })}
	{@const appData = props.app}
	<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
		<h3 class="text-sm font-semibold mb-4">Resource Limits</h3>
		<div class="grid grid-cols-2 gap-4">
			<div>
				<label for="cpu" class="block text-xs mb-1" style="color: var(--color-text-muted);">CPU Limit (cores)</label>
				<select id="cpu" class="w-full rounded px-3 py-2 text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
					onchange={async (e) => {
						const val = parseFloat((e.target as HTMLSelectElement).value);
						await api.updateAppResources(props.projectId, appData.id, { cpu_limit: val, memory_limit: appData.memory_limit });
						await props.onUpdate();
					}}>
					<option value="0" selected={appData.cpu_limit === 0}>No limit</option>
					<option value="0.25" selected={appData.cpu_limit === 0.25}>0.25 CPU</option>
					<option value="0.5" selected={appData.cpu_limit === 0.5}>0.5 CPU</option>
					<option value="1" selected={appData.cpu_limit === 1}>1 CPU</option>
					<option value="2" selected={appData.cpu_limit === 2}>2 CPU</option>
					<option value="4" selected={appData.cpu_limit === 4}>4 CPU</option>
				</select>
			</div>
			<div>
				<label for="mem" class="block text-xs mb-1" style="color: var(--color-text-muted);">Memory Limit</label>
				<select id="mem" class="w-full rounded px-3 py-2 text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
					onchange={async (e) => {
						const val = parseInt((e.target as HTMLSelectElement).value);
						await api.updateAppResources(props.projectId, appData.id, { cpu_limit: appData.cpu_limit, memory_limit: val });
						await props.onUpdate();
					}}>
					<option value="0" selected={appData.memory_limit === 0}>No limit</option>
					<option value={128 * 1024 * 1024} selected={appData.memory_limit === 128 * 1024 * 1024}>128 MB</option>
					<option value={256 * 1024 * 1024} selected={appData.memory_limit === 256 * 1024 * 1024}>256 MB</option>
					<option value={512 * 1024 * 1024} selected={appData.memory_limit === 512 * 1024 * 1024}>512 MB</option>
					<option value={1024 * 1024 * 1024} selected={appData.memory_limit === 1024 * 1024 * 1024}>1 GB</option>
					<option value={2 * 1024 * 1024 * 1024} selected={appData.memory_limit === 2 * 1024 * 1024 * 1024}>2 GB</option>
					<option value={4 * 1024 * 1024 * 1024} selected={appData.memory_limit === 4 * 1024 * 1024 * 1024}>4 GB</option>
				</select>
			</div>
		</div>
	</div>
{/snippet}

{#snippet HealthCheckSettings(props: { app: App; projectId: string; onUpdate: () => Promise<void> })}
	{@const appData = props.app}
	<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
		<h3 class="text-sm font-semibold mb-4">Health Check</h3>
		<div class="grid grid-cols-2 gap-4">
			<div>
				<label for="hc-path" class="block text-xs mb-1" style="color: var(--color-text-muted);">Health Check Path</label>
				<input id="hc-path" type="text" value={appData.health_check_path} placeholder="/health"
					class="w-full rounded px-3 py-2 text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
					onblur={async (e) => {
						const val = (e.target as HTMLInputElement).value;
						if (val !== appData.health_check_path) {
							await api.updateAppHealthCheck(props.projectId, appData.id, { path: val, interval: appData.health_check_interval || 30 });
							await props.onUpdate();
						}
					}} />
			</div>
			<div>
				<label for="hc-interval" class="block text-xs mb-1" style="color: var(--color-text-muted);">Interval (seconds)</label>
				<input id="hc-interval" type="number" value={appData.health_check_interval || 30} min="5" max="300"
					class="w-full rounded px-3 py-2 text-sm" style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
					onblur={async (e) => {
						const val = parseInt((e.target as HTMLInputElement).value);
						if (val !== appData.health_check_interval) {
							await api.updateAppHealthCheck(props.projectId, appData.id, { path: appData.health_check_path, interval: val });
							await props.onUpdate();
						}
					}} />
			</div>
		</div>
	</div>
{/snippet}
