import { createApi } from '$lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch, params }) => {
	const api = createApi(fetch);
	const [project, apps, databases] = await Promise.all([
		api.getProject(params.id),
		api.listApps(params.id),
		api.listDatabases(params.id),
	]);
	return { project, apps, databases };
};
