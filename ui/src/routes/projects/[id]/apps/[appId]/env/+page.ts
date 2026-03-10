import { createApi } from '$lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch, params }) => {
	const api = createApi(fetch);
	const [app, envVars] = await Promise.all([
		api.getApp(params.id, params.appId),
		api.listEnvVars(params.id, params.appId),
	]);
	return { app, envVars, projectId: params.id, appId: params.appId };
};
