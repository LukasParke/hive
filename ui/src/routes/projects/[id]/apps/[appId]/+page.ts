import { createApi } from '$lib/api';
import type { SwarmNode } from '$lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch, params }) => {
	const api = createApi(fetch);
	const [app, deployments, tasks, events, ports, serviceLinks, previews, nodesData] = await Promise.all([
		api.getApp(params.id, params.appId),
		api.listDeployments(params.id, params.appId),
		api.getAppTasks(params.id, params.appId).catch(() => []),
		api.getAppEvents(params.id, params.appId).catch(() => []),
		api.getAppPorts(params.id, params.appId).catch(() => []),
		api.listServiceLinks(params.id, params.appId).catch(() => []),
		api.listPreviews(params.id, params.appId).catch(() => []),
		api.listNodes().catch(() => ({ nodes: [] as SwarmNode[] })),
	]);
	return { app, deployments, tasks, events, ports, serviceLinks, previews, nodes: nodesData.nodes, projectId: params.id, appId: params.appId };
};
