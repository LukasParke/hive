import { createApi } from '$lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch, params }) => {
	const api = createApi(fetch);
	const [volumes, apps] = await Promise.all([
		api.listVolumes(params.id),
		api.listApps(params.id),
	]);
	return { volumes, apps, projectId: params.id };
};
