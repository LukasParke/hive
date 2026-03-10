<script lang="ts">
	import { api } from '$lib/api';
	import { tick } from 'svelte';

	let { data } = $props();

	let logs = $state<{ id: number; app_id: string; service_name: string; node_id: string; stream: string; message: string; level: string; timestamp: string }[]>([]);
	let error = $state('');
	let loading = $state(true);
	let search = $state('');
	let level = $state('');
	let timeRange = $state<'1h' | '6h' | '24h' | '7d' | 'custom'>('24h');
	let customSince = $state('');
	let customUntil = $state('');
	let autoScroll = $state(true);
	let logContainer: HTMLDivElement;

	const filteredLogs = $derived(
		logs.filter((l) => {
			if (search && !l.message?.toLowerCase().includes(search.toLowerCase())) return false;
			if (level && l.level?.toLowerCase() !== level.toLowerCase()) return false;
			return true;
		})
	);

	$effect(() => {
		if (data?.projectId && data?.appId && data?.app) {
			loadLogs();
		}
	});

	$effect(() => {
		const interval = setInterval(() => {
			if (data?.projectId && data?.appId && data?.app && autoScroll) loadLogs();
		}, 5000);
		return () => clearInterval(interval);
	});

	function getTimeParams(): { since?: string; until?: string } {
		const now = new Date();
		let since: Date | null = null;
		if (timeRange === '1h') since = new Date(now.getTime() - 60 * 60 * 1000);
		else if (timeRange === '6h') since = new Date(now.getTime() - 6 * 60 * 60 * 1000);
		else if (timeRange === '24h') since = new Date(now.getTime() - 24 * 60 * 60 * 1000);
		else if (timeRange === '7d') since = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
		else if (timeRange === 'custom' && customSince) {
			since = new Date(customSince);
		}
		let until: Date | null = null;
		if (timeRange === 'custom' && customUntil) until = new Date(customUntil);
		const out: { since?: string; until?: string } = {};
		if (since) out.since = since.toISOString();
		if (until) out.until = until.toISOString();
		return out;
	}

	async function loadLogs() {
		if (!data?.projectId || !data?.appId) return;
		try {
			loading = true;
			const time = getTimeParams();
			logs = await api.queryAppLogs(data.projectId, data.appId, {
				...time,
				limit: 1000,
			});
			error = '';
			if (autoScroll && logContainer) {
				await tick();
				logContainer.scrollTop = logContainer.scrollHeight;
			}
		} catch (e: any) {
			error = e.message;
		} finally {
			loading = false;
		}
	}

	function levelColor(lvl: string): string {
		switch (lvl?.toLowerCase()) {
			case 'error':
				return 'var(--color-danger)';
			case 'warn':
			case 'warning':
				return 'var(--color-warning)';
			case 'info':
			case 'debug':
				return 'var(--color-primary)';
			default:
				return 'var(--color-text-muted)';
		}
	}

	function formatTimestamp(ts: string): string {
		try {
			return new Date(ts).toLocaleTimeString('en-GB', {
				hour: '2-digit',
				minute: '2-digit',
				second: '2-digit',
				hour12: false,
			});
		} catch {
			return ts;
		}
	}
</script>

<svelte:head><title>Logs | Hive</title></svelte:head>

<div class="max-w-6xl mx-auto p-6">
	<div class="flex items-center justify-between mb-6">
		<div>
			<a href="/projects" class="text-sm" style="color: var(--color-text-muted);">Projects</a>
			<span class="text-sm" style="color: var(--color-text-muted);"> / </span>
			<a href="/projects/{data.projectId}" class="text-sm" style="color: var(--color-text-muted);">{data.projectId}</a>
			<span class="text-sm" style="color: var(--color-text-muted);"> / </span>
			<a href="/projects/{data.projectId}/apps/{data.appId}" class="text-sm" style="color: var(--color-text-muted);">{data.app?.name ?? 'App'}</a>
			<span class="text-sm" style="color: var(--color-text-muted);"> / </span>
			<h2 class="text-2xl font-bold inline">Logs</h2>
		</div>
		<a
			href="/projects/{data.projectId}/apps/{data.appId}"
			class="text-sm px-3 py-1.5 rounded"
			style="border: 1px solid var(--color-border); color: var(--color-text-muted);"
			>Back to app</a
		>
	</div>

	{#if error}
		<div
			class="rounded-lg p-4 mb-4"
			style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);"
		>
			<p style="color: var(--color-danger);">{error}</p>
		</div>
	{/if}

	<!-- Controls -->
	<div
		class="rounded-lg p-4 mb-4 flex flex-wrap gap-4 items-end"
		style="background-color: var(--color-surface); border: 1px solid var(--color-border);"
	>
		<div class="flex-1 min-w-[200px]">
			<label class="block text-xs mb-1" style="color: var(--color-text-muted);">Search</label>
			<input
				type="text"
				bind:value={search}
				placeholder="Filter logs..."
				class="w-full rounded px-3 py-2 text-sm font-mono"
				style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
			/>
		</div>
		<div>
			<label class="block text-xs mb-1" style="color: var(--color-text-muted);">Level</label>
			<select
				bind:value={level}
				class="rounded px-3 py-2 text-sm"
				style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
				onchange={() => loadLogs()}
			>
				<option value="">All</option>
				<option value="info">Info</option>
				<option value="warn">Warn</option>
				<option value="error">Error</option>
			</select>
		</div>
		<div>
			<label class="block text-xs mb-1" style="color: var(--color-text-muted);">Time range</label>
			<select
				bind:value={timeRange}
				class="rounded px-3 py-2 text-sm"
				style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
				onchange={() => loadLogs()}
			>
				<option value="1h">Last 1 hour</option>
				<option value="6h">Last 6 hours</option>
				<option value="24h">Last 24 hours</option>
				<option value="7d">Last 7 days</option>
				<option value="custom">Custom</option>
			</select>
		</div>
		{#if timeRange === 'custom'}
			<div>
				<label class="block text-xs mb-1" style="color: var(--color-text-muted);">From</label>
				<input
					type="datetime-local"
					bind:value={customSince}
					class="rounded px-3 py-2 text-sm"
					style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
					onchange={() => loadLogs()}
				/>
			</div>
			<div>
				<label class="block text-xs mb-1" style="color: var(--color-text-muted);">To</label>
				<input
					type="datetime-local"
					bind:value={customUntil}
					class="rounded px-3 py-2 text-sm"
					style="background-color: var(--color-bg); border: 1px solid var(--color-border);"
					onchange={() => loadLogs()}
				/>
			</div>
		{/if}
		<div class="flex items-center gap-2">
			<label class="flex items-center gap-2 cursor-pointer text-sm">
				<input type="checkbox" bind:checked={autoScroll} />
				<span style="color: var(--color-text-muted);">Auto-scroll</span>
			</label>
		</div>
		<button
			onclick={() => loadLogs()}
			disabled={loading}
			class="px-4 py-2 rounded text-sm font-medium text-white"
			style="background-color: var(--color-primary); opacity: loading ? 0.6 : 1;"
		>
			{loading ? 'Loading...' : 'Refresh'}
		</button>
	</div>

	<!-- Log output -->
	<div
		bind:this={logContainer}
		class="rounded-lg font-mono text-xs overflow-auto"
		style="
			background-color: var(--color-bg);
			color: var(--color-text);
			min-height: 400px;
			max-height: 70vh;
			border: 1px solid var(--color-border);
			padding: 1rem;
		"
	>
		{#if filteredLogs.length === 0 && !loading}
			<p style="color: var(--color-text-muted);">No log entries. Logs are stored when the log aggregation pipeline is running.</p>
		{:else}
			{#each filteredLogs as entry}
				<div class="flex gap-3 py-0.5 hover:bg-white/5 px-1 -mx-1 rounded">
					<span style="color: var(--color-text-muted); flex-shrink: 0;">{formatTimestamp(entry.timestamp)}</span>
					<span
						class="uppercase font-semibold w-10 flex-shrink-0"
						style="color: {levelColor(entry.level)};"
						>{entry.level || 'info'}</span
					>
					{#if entry.service_name}
						<span style="color: var(--color-text-muted); flex-shrink: 0;">[{entry.service_name}]</span>
					{/if}
					<span class="break-all flex-1">{entry.message}</span>
				</div>
			{/each}
		{/if}
	</div>
</div>
