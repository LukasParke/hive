import { createApi } from '$lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const api = createApi(fetch);
	const [templates, projects] = await Promise.all([
		api.listTemplates(),
		api.listProjects(),
	]);
	return { templates, projects };
};
