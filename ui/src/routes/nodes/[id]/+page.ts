import { createApi } from '$lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch, params }) => {
	const api = createApi(fetch);

	const [nodesData, promNodes, history] = await Promise.all([
		api.listNodes().catch(() => ({ nodes: [] })),
		api.metricsNodes().catch(() => []),
		api.metricsNodeHistory(params.id, '1h').catch(() => ({ hostname: params.id, cpu: [], mem: [] }))
	]);

	const swarmNode = (nodesData.nodes ?? []).find(
		(n) => n.ID === params.id || n.Description.Hostname === params.id
	);

	const promNode = promNodes.find(
		(p) => p.hostname === params.id || p.nodeId === params.id ||
			(swarmNode && p.hostname === swarmNode.Description.Hostname)
	);

	return {
		nodeId: params.id,
		hostname: swarmNode?.Description.Hostname ?? promNode?.hostname ?? params.id,
		swarmNode: swarmNode ?? null,
		promNode: promNode ?? null,
		history
	};
};
