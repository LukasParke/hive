import type { NodeUpdateStatus, ServiceUpdateStatus, UpdateEvent } from '$lib/types';

interface UpdateProgress {
	node_id: string;
	action: string;
	status: string;
	output: string;
	progress: number;
	timestamp: number;
}

interface ServiceUpdateProgress {
	service_name: string;
	status: string;
	message: string;
	progress: number;
}

interface UpdatesState {
	nodeStatuses: Map<string, NodeUpdateStatus>;
	serviceStatuses: Map<string, ServiceUpdateStatus>;
	activeNodeOperations: Map<string, UpdateProgress>;
	activeServiceOperations: Map<string, ServiceUpdateProgress>;
	nodeOutputLog: Map<string, string[]>;
	connected: boolean;
	error: string | null;
}

const MAX_RETRIES = 30;
const MAX_LOG_LINES = 500;

function createUpdatesStore() {
	let state = $state<UpdatesState>({
		nodeStatuses: new Map(),
		serviceStatuses: new Map(),
		activeNodeOperations: new Map(),
		activeServiceOperations: new Map(),
		nodeOutputLog: new Map(),
		connected: false,
		error: null
	});

	let ws: WebSocket | null = null;
	let subscribers = 0;
	let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
	let reconnectDelay = 1000;
	let retryCount = 0;

	function getWSUrl() {
		const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
		return `${proto}//${location.host}/ws/updates`;
	}

	function connect() {
		if (ws) return;

		if (retryCount >= MAX_RETRIES) {
			state.error = 'Updates feed unavailable';
			state.connected = false;
			return;
		}

		try {
			ws = new WebSocket(getWSUrl());
		} catch {
			scheduleReconnect();
			return;
		}

		ws.onopen = () => {
			state.connected = true;
			state.error = null;
			reconnectDelay = 1000;
			retryCount = 0;
		};

		ws.onclose = () => {
			ws = null;
			state.connected = false;
			if (subscribers > 0) scheduleReconnect();
		};

		ws.onerror = () => {
			state.error = 'Connection error';
		};

		ws.onmessage = (event) => {
			try {
				const msg = JSON.parse(event.data);
				handleMessage(msg);
			} catch { /* ignore malformed */ }
		};
	}

	function handleMessage(msg: any) {
		switch (msg.type) {
			case 'node_update_status': {
				const payload = msg.payload as NodeUpdateStatus;
				state.nodeStatuses = new Map(state.nodeStatuses).set(payload.node_id, payload);
				break;
			}
			case 'update_progress': {
				const p = msg.payload as UpdateProgress;
				state.activeNodeOperations = new Map(state.activeNodeOperations).set(p.node_id, p);

				if (p.output) {
					const log = state.nodeOutputLog.get(p.node_id) || [];
					log.push(p.output);
					if (log.length > MAX_LOG_LINES) log.splice(0, log.length - MAX_LOG_LINES);
					state.nodeOutputLog = new Map(state.nodeOutputLog).set(p.node_id, log);
				}

				if (p.status === 'completed' || p.status === 'failed') {
					setTimeout(() => {
						const ops = new Map(state.activeNodeOperations);
						ops.delete(p.node_id);
						state.activeNodeOperations = ops;
					}, 5000);
				}
				break;
			}
			case 'service_update_available': {
				const p = msg.payload;
				const existing = state.serviceStatuses.get(p.service_name);
				if (existing) {
					existing.update_available = true;
					existing.latest_version = p.latest_version;
					state.serviceStatuses = new Map(state.serviceStatuses).set(p.service_name, existing);
				}
				break;
			}
			case 'service_update_progress': {
				const p = msg.payload as ServiceUpdateProgress;
				state.activeServiceOperations = new Map(state.activeServiceOperations).set(p.service_name, p);
				if (p.status === 'completed' || p.status === 'failed' || p.status === 'rolled_back') {
					setTimeout(() => {
						const ops = new Map(state.activeServiceOperations);
						ops.delete(p.service_name);
						state.activeServiceOperations = ops;
					}, 5000);
				}
				break;
			}
		}
	}

	function scheduleReconnect() {
		if (reconnectTimer) return;
		retryCount++;
		reconnectTimer = setTimeout(() => {
			reconnectTimer = null;
			connect();
		}, Math.min(reconnectDelay, 30000));
		reconnectDelay *= 1.5;
	}

	function disconnect() {
		if (reconnectTimer) {
			clearTimeout(reconnectTimer);
			reconnectTimer = null;
		}
		if (ws) {
			ws.onclose = null;
			ws.close();
			ws = null;
		}
		state.connected = false;
	}

	return {
		get state() { return state; },

		subscribe() {
			subscribers++;
			if (subscribers === 1) connect();
		},

		unsubscribe() {
			subscribers = Math.max(0, subscribers - 1);
			if (subscribers === 0) disconnect();
		},

		clearNodeLog(nodeId: string) {
			const log = new Map(state.nodeOutputLog);
			log.delete(nodeId);
			state.nodeOutputLog = log;
		},

		seedNodeStatuses(statuses: NodeUpdateStatus[]) {
			const map = new Map(state.nodeStatuses);
			for (const s of statuses) {
				map.set(s.node_id, s);
			}
			state.nodeStatuses = map;
		},

		seedServiceStatuses(statuses: ServiceUpdateStatus[]) {
			const map = new Map(state.serviceStatuses);
			for (const s of statuses) {
				map.set(s.service_name, s);
			}
			state.serviceStatuses = map;
		}
	};
}

export const updatesStore = createUpdatesStore();
