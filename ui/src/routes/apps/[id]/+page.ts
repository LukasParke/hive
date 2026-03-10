import { createApi } from '$lib/api';
import type { PageLoad } from './$types';
import { redirect } from '@sveltejs/kit';

export const load: PageLoad = async ({ fetch, params, url }) => {
	const api = createApi(fetch);
	let projectId = url.searchParams.get('project') || '';
	if (!projectId) {
		const apps = await api.listAllApps();
		const app = apps.find((a) => a.id === params.id);
		if (app?.project_id) {
			projectId = app.project_id;
		}
	}
	if (!projectId) {
		throw redirect(302, '/apps');
	}
	throw redirect(302, `/projects/${projectId}/apps/${params.id}`);
};
