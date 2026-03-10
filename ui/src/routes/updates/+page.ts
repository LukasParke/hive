import { createApi } from '$lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const api = createApi(fetch);
	const [summary, nodeStatuses, serviceStatuses, history, policies] = await Promise.all([
		api.updatesSummary(),
		api.updatesNodes(),
		api.updatesServices(),
		api.updatesHistory({ limit: 50 }),
		api.listUpdatePolicies().catch(() => [])
	]);
	return { summary, nodeStatuses, serviceStatuses, history, policies };
};
