import { env } from '$env/dynamic/private';

const PROM_URL = env.PROMETHEUS_URL || 'http://hive-prometheus:9090';

interface PromInstantResult {
	metric: Record<string, string>;
	value: [number, string]; // [unix_timestamp, value_string]
}

interface PromRangeResult {
	metric: Record<string, string>;
	values: [number, string][]; // [[unix_timestamp, value_string], ...]
}

interface PromResponse<T> {
	status: string;
	data: {
		resultType: string;
		result: T[];
	};
}

async function promQuery(promql: string): Promise<PromInstantResult[]> {
	const url = `${PROM_URL}/api/v1/query?query=${encodeURIComponent(promql)}`;
	const res = await fetch(url, { signal: AbortSignal.timeout(10_000) });
	if (!res.ok) throw new Error(`Prometheus query failed: ${res.status}`);
	const body: PromResponse<PromInstantResult> = await res.json();
	if (body.status !== 'success') throw new Error('Prometheus query failed');
	return body.data.result;
}

async function promQueryRange(
	promql: string,
	start: number,
	end: number,
	step: string
): Promise<PromRangeResult[]> {
	const url = `${PROM_URL}/api/v1/query_range?query=${encodeURIComponent(promql)}&start=${start}&end=${end}&step=${step}`;
	const res = await fetch(url, { signal: AbortSignal.timeout(10_000) });
	if (!res.ok) throw new Error(`Prometheus range query failed: ${res.status}`);
	const body: PromResponse<PromRangeResult> = await res.json();
	if (body.status !== 'success') throw new Error('Prometheus range query failed');
	return body.data.result;
}

function val(result: PromInstantResult[], fallback = 0): number {
	if (!result.length) return fallback;
	return parseFloat(result[0].value[1]) || fallback;
}

function valByInstance(result: PromInstantResult[]): Map<string, number> {
	const map = new Map<string, number>();
	for (const r of result) {
		const instance = r.metric.instance || r.metric.node_hostname || '';
		map.set(instance, parseFloat(r.value[1]) || 0);
	}
	return map;
}

// ── Exported types ──────────────────────────────────────────────────

export interface ClusterSummary {
	nodes: number;
	nodesUp: number;
	totalCores: number;
	totalRAM: number;
	totalDisk: number;
	usedDisk: number;
	avgCPU: number;
	containers: number;
}

export interface NodeCurrent {
	hostname: string;
	nodeId: string;
	up: boolean;
	cpuPct: number;
	cores: number;
	memUsed: number;
	memTotal: number;
	diskUsed: number;
	diskTotal: number;
	uptimeSeconds: number;
	tempCelsius: number;
	containersRunning: number;
	loadAvg1: number;
}

export interface TimeSeriesPoint {
	ts: number;
	value: number;
}

export interface NodeHistory {
	hostname: string;
	cpu: TimeSeriesPoint[];
	mem: TimeSeriesPoint[];
}

// ── Query helpers ───────────────────────────────────────────────────

async function safeQuery(promql: string): Promise<PromInstantResult[]> {
	try {
		return await promQuery(promql);
	} catch {
		return [];
	}
}

async function safeQueryRange(
	promql: string,
	start: number,
	end: number,
	step: string
): Promise<PromRangeResult[]> {
	try {
		return await promQueryRange(promql, start, end, step);
	} catch {
		return [];
	}
}

// ── Public API ──────────────────────────────────────────────────────

export async function getClusterSummary(): Promise<ClusterSummary> {
	const [
		upResult,
		coresResult,
		ramResult,
		diskTotalResult,
		diskUsedResult,
		cpuResult,
		containersResult
	] = await Promise.all([
		safeQuery('up{job="node-exporter"}'),
		safeQuery('count by (instance)(node_cpu_seconds_total{mode="idle"})'),
		safeQuery('node_memory_MemTotal_bytes'),
		safeQuery('sum by (instance)(node_filesystem_size_bytes{mountpoint="/",fstype!="tmpfs"})'),
		safeQuery('sum by (instance)(node_filesystem_size_bytes{mountpoint="/",fstype!="tmpfs"} - node_filesystem_avail_bytes{mountpoint="/",fstype!="tmpfs"})'),
		safeQuery('100 - (avg by (instance)(rate(node_cpu_seconds_total{mode="idle"}[1m])) * 100)'),
		safeQuery('sum(count by (instance)(container_last_seen{image!=""}))')
	]);

	const nodesUp = upResult.filter(r => r.value[1] === '1').length;
	const totalCores = Array.from(valByInstance(coresResult).values()).reduce((a, b) => a + b, 0);
	const totalRAM = Array.from(valByInstance(ramResult).values()).reduce((a, b) => a + b, 0);
	const totalDisk = Array.from(valByInstance(diskTotalResult).values()).reduce((a, b) => a + b, 0);
	const usedDisk = Array.from(valByInstance(diskUsedResult).values()).reduce((a, b) => a + b, 0);
	const cpuValues = Array.from(valByInstance(cpuResult).values());
	const avgCPU = cpuValues.length > 0 ? cpuValues.reduce((a, b) => a + b, 0) / cpuValues.length : 0;

	return {
		nodes: upResult.length,
		nodesUp,
		totalCores,
		totalRAM,
		totalDisk,
		usedDisk,
		avgCPU,
		containers: val(containersResult)
	};
}

export async function getNodeMetrics(): Promise<NodeCurrent[]> {
	const [
		upResult,
		cpuResult,
		coresResult,
		memTotalResult,
		memAvailResult,
		diskTotalResult,
		diskAvailResult,
		uptimeResult,
		tempResult,
		containersResult,
		loadResult
	] = await Promise.all([
		safeQuery('up{job="node-exporter"}'),
		safeQuery('100 - (avg by (instance)(rate(node_cpu_seconds_total{mode="idle"}[1m])) * 100)'),
		safeQuery('count by (instance)(node_cpu_seconds_total{mode="idle"})'),
		safeQuery('node_memory_MemTotal_bytes'),
		safeQuery('node_memory_MemAvailable_bytes'),
		safeQuery('sum by (instance)(node_filesystem_size_bytes{mountpoint="/",fstype!="tmpfs"})'),
		safeQuery('sum by (instance)(node_filesystem_avail_bytes{mountpoint="/",fstype!="tmpfs"})'),
		safeQuery('node_time_seconds - node_boot_time_seconds'),
		safeQuery('node_hwmon_temp_celsius'),
		safeQuery('count by (instance)(container_last_seen{image!=""})'),
		safeQuery('node_load1')
	]);

	const upMap = new Map<string, boolean>();
	for (const r of upResult) {
		upMap.set(r.metric.instance || r.metric.node_hostname || '', r.value[1] === '1');
	}

	const cpuMap = valByInstance(cpuResult);
	const coresMap = valByInstance(coresResult);
	const memTotalMap = valByInstance(memTotalResult);
	const memAvailMap = valByInstance(memAvailResult);
	const diskTotalMap = valByInstance(diskTotalResult);
	const diskAvailMap = valByInstance(diskAvailResult);
	const uptimeMap = valByInstance(uptimeResult);
	const loadMap = valByInstance(loadResult);

	const tempMap = new Map<string, number>();
	for (const r of tempResult) {
		const inst = r.metric.instance || r.metric.node_hostname || '';
		const temp = parseFloat(r.value[1]) || 0;
		const existing = tempMap.get(inst) ?? 0;
		if (temp > existing) tempMap.set(inst, temp);
	}

	const containerMap = valByInstance(containersResult);
	const hostnames = new Set<string>();

	for (const r of upResult) {
		hostnames.add(r.metric.instance || r.metric.node_hostname || '');
	}

	const nodes: NodeCurrent[] = [];
	for (const hostname of hostnames) {
		const memTotal = memTotalMap.get(hostname) ?? 0;
		const memAvail = memAvailMap.get(hostname) ?? 0;
		const dTotal = diskTotalMap.get(hostname) ?? 0;
		const dAvail = diskAvailMap.get(hostname) ?? 0;

		const nodeIdResult = upResult.find(r =>
			(r.metric.instance || r.metric.node_hostname) === hostname
		);

		nodes.push({
			hostname,
			nodeId: nodeIdResult?.metric.node_id || hostname,
			up: upMap.get(hostname) ?? false,
			cpuPct: cpuMap.get(hostname) ?? 0,
			cores: coresMap.get(hostname) ?? 0,
			memUsed: memTotal - memAvail,
			memTotal,
			diskUsed: dTotal - dAvail,
			diskTotal: dTotal,
			uptimeSeconds: uptimeMap.get(hostname) ?? 0,
			tempCelsius: tempMap.get(hostname) ?? 0,
			containersRunning: containerMap.get(hostname) ?? 0,
			loadAvg1: loadMap.get(hostname) ?? 0
		});
	}

	return nodes;
}

export async function getNodeHistory(
	hostname: string,
	rangeSec = 3600,
	stepSec = 15
): Promise<NodeHistory> {
	const now = Math.floor(Date.now() / 1000);
	const start = now - rangeSec;
	const step = `${stepSec}s`;

	const instanceFilter = `instance="${hostname}"`;

	const [cpuResult, memResult] = await Promise.all([
		safeQueryRange(
			`100 - (avg by (instance)(rate(node_cpu_seconds_total{mode="idle",${instanceFilter}}[30s])) * 100)`,
			start, now, step
		),
		safeQueryRange(
			`100 * (1 - node_memory_MemAvailable_bytes{${instanceFilter}} / node_memory_MemTotal_bytes{${instanceFilter}})`,
			start, now, step
		)
	]);

	function toPoints(results: PromRangeResult[]): TimeSeriesPoint[] {
		if (!results.length) return [];
		return results[0].values.map(([ts, v]) => ({
			ts,
			value: parseFloat(v) || 0
		}));
	}

	return {
		hostname,
		cpu: toPoints(cpuResult),
		mem: toPoints(memResult)
	};
}

export interface ContainerMetrics {
	name: string;
	image: string;
	instance: string;
	cpuPct: number;
	memBytes: number;
}

function cleanImageName(raw: string): string {
	let img = raw.split('@')[0];
	img = img.replace(/^(?:127\.0\.0\.1|localhost)(?::\d+)?\//, '');
	const lastColon = img.lastIndexOf(':');
	if (lastColon > 0) {
		img = img.substring(0, lastColon);
	}
	return img || raw;
}

export async function getTopContainers(limit = 10): Promise<ContainerMetrics[]> {
	const groupBy = 'name, image, instance, container_label_com_docker_swarm_service_name';
	const [cpuResult, memResult] = await Promise.all([
		safeQuery(`sum by (${groupBy})(rate(container_cpu_usage_seconds_total{image!=""}[1m])) * 100`),
		safeQuery(`sum by (${groupBy})(container_memory_usage_bytes{image!=""})`)
	]);

	const cpuMap = new Map<string, { cpuPct: number; image: string; instance: string; serviceName: string }>();
	for (const r of cpuResult) {
		const rawName = r.metric.name || '';
		const serviceName = r.metric.container_label_com_docker_swarm_service_name || '';
		cpuMap.set(rawName, {
			cpuPct: parseFloat(r.value[1]) || 0,
			image: r.metric.image || '',
			instance: r.metric.instance || '',
			serviceName
		});
	}

	const memMap = new Map<string, number>();
	for (const r of memResult) {
		memMap.set(r.metric.name || '', parseFloat(r.value[1]) || 0);
	}

	const containers: ContainerMetrics[] = [];
	for (const [rawName, cpu] of cpuMap) {
		containers.push({
			name: cpu.serviceName || rawName,
			image: cleanImageName(cpu.image),
			instance: cpu.instance,
			cpuPct: cpu.cpuPct,
			memBytes: memMap.get(rawName) ?? 0
		});
	}

	containers.sort((a, b) => b.cpuPct - a.cpuPct);
	return containers.slice(0, limit);
}

export async function getNodeContainers(hostname: string): Promise<ContainerMetrics[]> {
	const instanceFilter = `instance="${hostname}"`;
	const svcLabel = 'container_label_com_docker_swarm_service_name';
	const groupBy = `name, image, ${svcLabel}`;
	const [cpuResult, memResult] = await Promise.all([
		safeQuery(`sum by (${groupBy})(rate(container_cpu_usage_seconds_total{image!="",${instanceFilter}}[1m])) * 100`),
		safeQuery(`sum by (${groupBy})(container_memory_usage_bytes{image!="",${instanceFilter}})`)
	]);

	const cpuMap = new Map<string, { cpuPct: number; image: string; serviceName: string }>();
	for (const r of cpuResult) {
		const rawName = r.metric.name || '';
		cpuMap.set(rawName, {
			cpuPct: parseFloat(r.value[1]) || 0,
			image: r.metric.image || '',
			serviceName: r.metric[svcLabel] || ''
		});
	}

	const memMap = new Map<string, number>();
	for (const r of memResult) {
		memMap.set(r.metric.name || '', parseFloat(r.value[1]) || 0);
	}

	const containers: ContainerMetrics[] = [];
	for (const [rawName, cpu] of cpuMap) {
		containers.push({
			name: cpu.serviceName || rawName,
			image: cleanImageName(cpu.image),
			instance: hostname,
			cpuPct: cpu.cpuPct,
			memBytes: memMap.get(rawName) ?? 0
		});
	}

	containers.sort((a, b) => b.cpuPct - a.cpuPct);
	return containers;
}

export async function isPrometheusReady(): Promise<boolean> {
	try {
		const res = await fetch(`${PROM_URL}/-/healthy`, {
			signal: AbortSignal.timeout(3_000)
		});
		return res.ok;
	} catch {
		return false;
	}
}
