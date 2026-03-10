import { createApi } from '$lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const api = createApi(fetch);
	const [statusData, nodesData, cluster, promNodes, serviceHealth, recentAudit, registryStatus] = await Promise.all([
		api.status(),
		api.listNodes().catch(() => ({ nodes: [] })),
		api.metricsCluster().catch(() => null),
		api.metricsNodes().catch(() => []),
		api.metricsServices().catch(() => []),
		api.listAuditLogs('limit=6').catch(() => []),
		api.registryStatus().catch(() => null),
	]);
	return {
		status: statusData,
		swarmNodes: nodesData.nodes ?? [],
		cluster: cluster ?? null,
		promNodes: Array.isArray(promNodes) ? promNodes : [],
		serviceHealth: Array.isArray(serviceHealth) ? serviceHealth : [],
		recentAudit: Array.isArray(recentAudit) ? recentAudit : [],
		registryStatus: registryStatus ?? null,
	};
};
