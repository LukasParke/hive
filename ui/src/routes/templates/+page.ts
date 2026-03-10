import { createApi } from '$lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const api = createApi(fetch);
	const [sources, customTemplates] = await Promise.all([
		api.listTemplateSources(),
		api.listCustomTemplates(),
	]);
	return { sources, customTemplates };
};
