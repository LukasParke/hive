import { createApi } from '$lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch, params }) => {
	const api = createApi(fetch);
	const result = await api.getCephCluster(params.clusterId);
	const [osds, pools] = await Promise.all([
		api.listCephOSDs(params.clusterId),
		api.listCephPools(params.clusterId),
	]);
	return { cluster: result.cluster, health: result.health, osds, pools, clusterId: params.clusterId };
};
