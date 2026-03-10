import { createApi } from '$lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch, params }) => {
	const api = createApi(fetch);
	const [secrets, apps] = await Promise.all([
		api.listSecrets(params.id),
		api.listApps(params.id),
	]);
	return { secrets, apps, projectId: params.id };
};
