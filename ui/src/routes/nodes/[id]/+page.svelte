<script lang="ts">
	import { api, type NodeMetricsReport } from '$lib/api';
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/stores';

	let nodeId = $derived($page.params.id);
	let latest = $state<NodeMetricsReport | null>(null);
	let history = $state<NodeMetricsReport[]>([]);
	let historyRange = $state('24h');
	let loading = $state(true);
	let error = $state('');
	let refreshInterval: ReturnType<typeof setInterval>;

	onMount(async () => {
		await loadData();
		refreshInterval = setInterval(loadLatest, 10000);
	});

	onDestroy(() => {
		clearInterval(refreshInterval);
	});

	async function loadData() {
		loading = true;
		try {
			const data = await api.getNodeMetrics(nodeId);
			latest = data.latest;
			history = data.history ?? [];
		} catch (e: any) {
			error = e.message;
		}
		loading = false;
	}

	async function loadLatest() {
		try {
			const data = await api.getNodeMetrics(nodeId);
			latest = data.latest;
		} catch {}
	}

	async function loadHistory() {
		try {
			history = await api.getNodeMetricsHistory(nodeId, historyRange);
		} catch {}
	}

	function formatBytes(bytes: number): string {
		if (!bytes) return '0 B';
		const k = 1024;
		const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
		const i = Math.floor(Math.log(bytes) / Math.log(k));
		return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
	}

	function pct(used: number, total: number): number {
		if (!total) return 0;
		return Math.round((used / total) * 100);
	}

	function barColor(pct: number): string {
		if (pct > 85) return '#ef4444';
		if (pct > 60) return '#f59e0b';
		return '#22c55e';
	}

	function formatUptime(seconds: number): string {
		const days = Math.floor(seconds / 86400);
		const hours = Math.floor((seconds % 86400) / 3600);
		const mins = Math.floor((seconds % 3600) / 60);
		if (days > 0) return `${days}d ${hours}h ${mins}m`;
		return `${hours}h ${mins}m`;
	}

	function formatRate(bytes: number): string {
		return formatBytes(bytes) + '/s';
	}

	function sparklineData(metric: (r: NodeMetricsReport) => number): number[] {
		return history.map(metric);
	}

	function sparklinePath(data: number[], width: number, height: number): string {
		if (data.length < 2) return '';
		const max = Math.max(...data, 1);
		const step = width / (data.length - 1);
		return data.map((v, i) => {
			const x = i * step;
			const y = height - (v / max) * height;
			return `${i === 0 ? 'M' : 'L'} ${x.toFixed(1)} ${y.toFixed(1)}`;
		}).join(' ');
	}
</script>

<div class="max-w-6xl mx-auto">
	<div class="mb-6">
		<a href="/" class="text-sm hover:underline" style="color: var(--color-primary);">Back to Dashboard</a>
	</div>

	{#if loading}
		<p style="color: var(--color-text-muted);">Loading node metrics...</p>
	{:else if error}
		<div class="rounded-lg p-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid #ef4444;">
			<p style="color: #ef4444;">{error}</p>
		</div>
	{:else if latest}
		<div class="flex items-center gap-4 mb-6">
			<h2 class="text-2xl font-bold">{latest.hostname}</h2>
			<span class="text-sm px-2 py-0.5 rounded" style="background-color: var(--color-surface); color: var(--color-text-muted);">
				{latest.os}
			</span>
			<span class="text-sm px-2 py-0.5 rounded" style="background-color: var(--color-surface); color: var(--color-text-muted);">
				Kernel {latest.kernel}
			</span>
		</div>

		<!-- CPU Section -->
		<section class="rounded-lg p-5 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="font-semibold text-lg mb-4">CPU ({latest.cpu_cores} cores)</h3>

			<div class="grid grid-cols-2 md:grid-cols-4 gap-4 mb-4">
				<div>
					<p class="text-xs mb-1" style="color: var(--color-text-muted);">Total Usage</p>
					<p class="text-2xl font-bold" style="color: {barColor(latest.cpu_total_pct)};">{latest.cpu_total_pct.toFixed(1)}%</p>
				</div>
				<div>
					<p class="text-xs mb-1" style="color: var(--color-text-muted);">Load Average</p>
					<p class="text-lg font-mono">{latest.load_avg_1.toFixed(2)} / {latest.load_avg_5.toFixed(2)} / {latest.load_avg_15.toFixed(2)}</p>
				</div>
				{#if latest.cpu_temp_celsius > 0}
					<div>
						<p class="text-xs mb-1" style="color: var(--color-text-muted);">Temperature</p>
						<p class="text-lg font-mono">{latest.cpu_temp_celsius.toFixed(0)}°C</p>
					</div>
				{/if}
			</div>

			{#if latest.cpu_per_core && latest.cpu_per_core.length > 0}
				<p class="text-xs mb-2" style="color: var(--color-text-muted);">Per-Core Usage</p>
				<div class="grid gap-1.5" style="grid-template-columns: repeat(auto-fill, minmax(80px, 1fr));">
					{#each latest.cpu_per_core as core, i}
						<div class="text-center">
							<div class="text-xs mb-0.5" style="color: var(--color-text-muted);">C{i}</div>
							<div class="w-full rounded-full h-3" style="background-color: var(--color-border);">
								<div class="h-3 rounded-full" style="width: {Math.min(core, 100)}%; background-color: {barColor(core)};"></div>
							</div>
							<div class="text-xs mt-0.5">{core.toFixed(0)}%</div>
						</div>
					{/each}
				</div>
			{/if}

			<!-- CPU sparkline -->
			{#if history.length > 1}
				<div class="mt-4">
					<p class="text-xs mb-1" style="color: var(--color-text-muted);">CPU History</p>
					<svg viewBox="0 0 300 50" class="w-full h-12" preserveAspectRatio="none">
						<path d={sparklinePath(sparklineData(r => r.cpu_total_pct), 300, 50)}
							fill="none" stroke="#22c55e" stroke-width="1.5" />
					</svg>
				</div>
			{/if}
		</section>

		<!-- Memory Section -->
		<section class="rounded-lg p-5 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="font-semibold text-lg mb-4">Memory</h3>
			{@const memPct = pct(latest.mem_used, latest.mem_total)}

			<div class="grid grid-cols-2 md:grid-cols-4 gap-4 mb-4">
				<div>
					<p class="text-xs mb-1" style="color: var(--color-text-muted);">Used / Total</p>
					<p class="text-lg font-mono">{formatBytes(latest.mem_used)} / {formatBytes(latest.mem_total)}</p>
				</div>
				<div>
					<p class="text-xs mb-1" style="color: var(--color-text-muted);">Available</p>
					<p class="text-lg font-mono">{formatBytes(latest.mem_available)}</p>
				</div>
				<div>
					<p class="text-xs mb-1" style="color: var(--color-text-muted);">Buffers / Cached</p>
					<p class="text-lg font-mono">{formatBytes(latest.mem_buffers)} / {formatBytes(latest.mem_cached)}</p>
				</div>
				<div>
					<p class="text-xs mb-1" style="color: var(--color-text-muted);">Swap</p>
					<p class="text-lg font-mono">{formatBytes(latest.swap_used)} / {formatBytes(latest.swap_total)}</p>
				</div>
			</div>

			<div class="w-full rounded-full h-4" style="background-color: var(--color-border);">
				<div class="h-4 rounded-full transition-all" style="width: {memPct}%; background-color: {barColor(memPct)};"></div>
			</div>
			<p class="text-xs mt-1 text-right" style="color: var(--color-text-muted);">{memPct}% used</p>

			{#if history.length > 1}
				<div class="mt-4">
					<p class="text-xs mb-1" style="color: var(--color-text-muted);">Memory History</p>
					<svg viewBox="0 0 300 50" class="w-full h-12" preserveAspectRatio="none">
						<path d={sparklinePath(sparklineData(r => r.mem_total > 0 ? (r.mem_used / r.mem_total) * 100 : 0), 300, 50)}
							fill="none" stroke="#3b82f6" stroke-width="1.5" />
					</svg>
				</div>
			{/if}
		</section>

		<!-- Disk Section -->
		<section class="rounded-lg p-5 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="font-semibold text-lg mb-4">Disks</h3>
			<div class="space-y-4">
				{#each latest.disks as disk}
					{@const dp = pct(disk.used, disk.total)}
					<div>
						<div class="flex justify-between text-sm mb-1">
							<span class="font-medium font-mono">{disk.mount_point}</span>
							<span style="color: var(--color-text-muted);">{disk.device} ({disk.fs_type})</span>
						</div>
						<div class="flex justify-between text-xs mb-1" style="color: var(--color-text-muted);">
							<span>{formatBytes(disk.used)} / {formatBytes(disk.total)}</span>
							<span style="color: {barColor(dp)};">{dp}%</span>
						</div>
						<div class="w-full rounded-full h-2.5" style="background-color: var(--color-border);">
							<div class="h-2.5 rounded-full" style="width: {dp}%; background-color: {barColor(dp)};"></div>
						</div>
						{#if disk.read_bytes > 0 || disk.write_bytes > 0}
							<div class="flex gap-4 mt-1 text-xs" style="color: var(--color-text-muted);">
								<span>Read: {formatBytes(disk.read_bytes)}</span>
								<span>Write: {formatBytes(disk.write_bytes)}</span>
							</div>
						{/if}
						{#if disk.smart_ok !== undefined && disk.smart_ok !== null}
							<div class="text-xs mt-1" style="color: {disk.smart_ok ? '#22c55e' : '#ef4444'};">
								SMART: {disk.smart_ok ? 'OK' : 'FAILING'}
							</div>
						{/if}
					</div>
				{/each}
			</div>
		</section>

		<!-- Network Section -->
		<section class="rounded-lg p-5 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="font-semibold text-lg mb-4">Network Interfaces</h3>
			<div class="space-y-3">
				{#each latest.interfaces as iface}
					<div class="rounded p-3" style="background-color: var(--color-bg);">
						<div class="flex items-center justify-between mb-2">
							<span class="font-medium font-mono">{iface.name}</span>
							{#if iface.link_speed_mbps > 0}
								<span class="text-xs" style="color: var(--color-text-muted);">{iface.link_speed_mbps >= 1000 ? (iface.link_speed_mbps / 1000).toFixed(0) + ' Gbps' : iface.link_speed_mbps + ' Mbps'}</span>
							{/if}
						</div>
						<div class="grid grid-cols-2 md:grid-cols-4 gap-3 text-xs">
							<div>
								<span style="color: var(--color-text-muted);">RX</span>
								<p class="font-mono">{formatBytes(iface.rx_bytes)}</p>
							</div>
							<div>
								<span style="color: var(--color-text-muted);">TX</span>
								<p class="font-mono">{formatBytes(iface.tx_bytes)}</p>
							</div>
							<div>
								<span style="color: var(--color-text-muted);">Packets</span>
								<p class="font-mono">{iface.rx_packets.toLocaleString()} / {iface.tx_packets.toLocaleString()}</p>
							</div>
							{#if iface.rx_errors > 0 || iface.tx_errors > 0}
								<div>
									<span style="color: #ef4444;">Errors</span>
									<p class="font-mono" style="color: #ef4444;">{iface.rx_errors} / {iface.tx_errors}</p>
								</div>
							{/if}
						</div>
					</div>
				{/each}
			</div>
		</section>

		<!-- System Section -->
		<section class="rounded-lg p-5 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="font-semibold text-lg mb-4">System</h3>
			<div class="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
				<div>
					<p class="text-xs mb-1" style="color: var(--color-text-muted);">OS</p>
					<p>{latest.os || 'Unknown'}</p>
				</div>
				<div>
					<p class="text-xs mb-1" style="color: var(--color-text-muted);">Kernel</p>
					<p class="font-mono text-xs">{latest.kernel}</p>
				</div>
				<div>
					<p class="text-xs mb-1" style="color: var(--color-text-muted);">Uptime</p>
					<p>{formatUptime(latest.uptime_seconds)}</p>
				</div>
				<div>
					<p class="text-xs mb-1" style="color: var(--color-text-muted);">Processes</p>
					<p>{latest.process_count}</p>
				</div>
				<div>
					<p class="text-xs mb-1" style="color: var(--color-text-muted);">Pending Updates</p>
					<p style="color: {latest.pending_updates > 0 ? '#f59e0b' : '#22c55e'};">{latest.pending_updates}</p>
				</div>
				<div>
					<p class="text-xs mb-1" style="color: var(--color-text-muted);">Containers (running/stopped)</p>
					<p>{latest.containers_running} / {latest.containers_stopped}</p>
				</div>
				<div>
					<p class="text-xs mb-1" style="color: var(--color-text-muted);">Images</p>
					<p>{latest.images_count}</p>
				</div>
				<div>
					<p class="text-xs mb-1" style="color: var(--color-text-muted);">Volumes</p>
					<p>{latest.volumes_count}</p>
				</div>
			</div>
		</section>

		<!-- GPU Section -->
		{#if latest.gpus && latest.gpus.length > 0}
			<section class="rounded-lg p-5 mb-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
				<h3 class="font-semibold text-lg mb-4">GPUs</h3>
				<div class="space-y-4">
					{#each latest.gpus as gpu}
						{@const gpuMemPct = pct(gpu.mem_used, gpu.mem_total)}
						<div class="rounded p-3" style="background-color: var(--color-bg);">
							<div class="flex items-center justify-between mb-2">
								<span class="font-medium">GPU {gpu.index}: {gpu.name}</span>
								<span class="text-sm">{gpu.temp_celsius.toFixed(0)}°C</span>
							</div>
							<div class="grid grid-cols-2 gap-4">
								<div>
									<div class="flex justify-between text-xs mb-1" style="color: var(--color-text-muted);">
										<span>Utilization</span>
										<span>{gpu.util_pct.toFixed(0)}%</span>
									</div>
									<div class="w-full rounded-full h-2" style="background-color: var(--color-border);">
										<div class="h-2 rounded-full" style="width: {gpu.util_pct}%; background-color: {barColor(gpu.util_pct)};"></div>
									</div>
								</div>
								<div>
									<div class="flex justify-between text-xs mb-1" style="color: var(--color-text-muted);">
										<span>VRAM</span>
										<span>{formatBytes(gpu.mem_used)} / {formatBytes(gpu.mem_total)}</span>
									</div>
									<div class="w-full rounded-full h-2" style="background-color: var(--color-border);">
										<div class="h-2 rounded-full" style="width: {gpuMemPct}%; background-color: {barColor(gpuMemPct)};"></div>
									</div>
								</div>
							</div>
						</div>
					{/each}
				</div>
			</section>
		{/if}

		<!-- History range selector -->
		{#if history.length > 0}
			<div class="flex items-center gap-3 mb-4">
				<p class="text-sm font-medium">Historical Range:</p>
				{#each ['1h', '6h', '24h', '7d'] as range}
					<button class="px-3 py-1 rounded text-sm"
						style="background-color: {historyRange === range ? 'var(--color-primary)' : 'var(--color-surface)'}; color: {historyRange === range ? 'white' : 'var(--color-text)'}; border: 1px solid var(--color-border);"
						on:click={() => { historyRange = range; loadHistory(); }}>
						{range}
					</button>
				{/each}
			</div>
		{/if}
	{/if}
</div>
