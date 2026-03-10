import { createApi } from '$lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch, params }) => {
	const api = createApi(fetch);
	return {
		app: await api.getApp(params.id, params.appId),
		projectId: params.id,
		appId: params.appId,
	};
};
