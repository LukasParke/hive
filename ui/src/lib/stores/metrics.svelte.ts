import type {
	PrometheusClusterSummary,
	PrometheusNodeCurrent,
	ServiceHealth
} from '$lib/api';

export interface ContainerMetric {
	name: string;
	image: string;
	instance: string;
	cpuPct: number;
	memBytes: number;
}

interface MetricsState {
	cluster: PrometheusClusterSummary | null;
	nodes: PrometheusNodeCurrent[];
	serviceHealth: ServiceHealth[];
	topContainers: ContainerMetric[];
	nodeHistory: Map<string, { cpu: number[]; mem: number[] }>;
	connected: boolean;
	lastUpdate: Date | null;
	error: string | null;
}

const MAX_RETRIES = 30;
const STABLE_THRESHOLD_MS = 5000;

function createMetricsStore() {
	let state = $state<MetricsState>({
		cluster: null,
		nodes: [],
		serviceHealth: [],
		topContainers: [],
		nodeHistory: new Map(),
		connected: false,
		lastUpdate: null,
		error: null
	});

	let ws: WebSocket | null = null;
	let subscribers = 0;
	let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
	let reconnectDelay = 1000;
	let retryCount = 0;
	let openedAt = 0;
	let visibilityHandler: (() => void) | null = null;
	let paused = false;

	function getWSUrl() {
		const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
		return `${proto}//${location.host}/ws/metrics`;
	}

	function connect() {
		if (ws || paused) return;

		if (retryCount >= MAX_RETRIES) {
			state.error = 'Metrics unavailable — too many failed attempts';
			state.connected = false;
			return;
		}

		try {
			ws = new WebSocket(getWSUrl());
		} catch {
			retryCount++;
			scheduleReconnect();
			return;
		}

		ws.onopen = () => {
			state.connected = true;
			state.error = null;
			openedAt = Date.now();
		};

		ws.onmessage = (e) => {
			try {
				const data = JSON.parse(e.data);
				if (data.type === 'metrics') {
					if (data.nodes) state.nodes = data.nodes;
					if (data.services) state.serviceHealth = data.services;
					if (data.cluster) state.cluster = data.cluster;
					if (data.topContainers) state.topContainers = data.topContainers;
					state.connected = true;
					state.error = null;
					state.lastUpdate = new Date();
				}
				if (data.type === 'history') {
					const map = new Map<string, { cpu: number[]; mem: number[] }>();
					for (const [hostname, hist] of Object.entries(data.data as Record<string, { cpu: number[]; mem: number[] }>)) {
						map.set(hostname, hist);
					}
					state.nodeHistory = map;
				}
			} catch {
				// Ignore malformed messages
			}
		};

		ws.onclose = () => {
			const wasStable = openedAt > 0 && Date.now() - openedAt >= STABLE_THRESHOLD_MS;
			ws = null;
			state.connected = false;
			openedAt = 0;

			if (wasStable) {
				reconnectDelay = 1000;
				retryCount = 0;
			} else {
				retryCount++;
			}

			if (subscribers > 0 && !paused) {
				scheduleReconnect();
			}
		};

		ws.onerror = () => {
			state.error = 'Metrics unavailable';
			state.connected = false;
		};
	}

	function scheduleReconnect() {
		if (reconnectTimer || paused) return;
		if (retryCount >= MAX_RETRIES) {
			state.error = 'Metrics unavailable — too many failed attempts';
			return;
		}
		reconnectTimer = setTimeout(() => {
			reconnectTimer = null;
			reconnectDelay = Math.min(reconnectDelay * 2, 30000);
			connect();
		}, reconnectDelay);
	}

	function disconnect() {
		if (reconnectTimer) {
			clearTimeout(reconnectTimer);
			reconnectTimer = null;
		}
		if (ws) {
			ws.close();
			ws = null;
		}
		openedAt = 0;
	}

	function setupVisibilityListener() {
		if (visibilityHandler || typeof document === 'undefined') return;
		visibilityHandler = () => {
			if (document.hidden) {
				paused = true;
				disconnect();
			} else {
				paused = false;
				if (subscribers > 0) {
					retryCount = 0;
					reconnectDelay = 1000;
					connect();
				}
			}
		};
		document.addEventListener('visibilitychange', visibilityHandler);
	}

	function teardownVisibilityListener() {
		if (visibilityHandler && typeof document !== 'undefined') {
			document.removeEventListener('visibilitychange', visibilityHandler);
			visibilityHandler = null;
		}
	}

	return {
		get state() { return state; },

		subscribe() {
			subscribers++;
			if (subscribers === 1) {
				retryCount = 0;
				reconnectDelay = 1000;
				paused = false;
				setupVisibilityListener();
				connect();
			}
			return () => {
				subscribers--;
				if (subscribers <= 0) {
					subscribers = 0;
					paused = false;
					disconnect();
					teardownVisibilityListener();
				}
			};
		},

		seedFromSSR(
			cluster: PrometheusClusterSummary | null,
			nodes: PrometheusNodeCurrent[],
			serviceHealth: ServiceHealth[]
		) {
			if (cluster && !state.cluster) {
				state.cluster = cluster;
			}
			if (nodes.length && !state.nodes.length) {
				state.nodes = nodes;
			}
			if (serviceHealth.length && !state.serviceHealth.length) {
				state.serviceHealth = serviceHealth;
			}
		}
	};
}

export const metricsStore = createMetricsStore();
