import { createApi } from '$lib/api';
import { safeJSONFetch, safeLoad } from '$lib/load-utils';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const api = createApi(fetch);
	const projects = await safeLoad(() => api.listProjects(), []);
	const projectId = projects.length > 0 ? projects[0].id : '';

	const activeRoutesReq = safeJSONFetch(fetch, '/api/v1/routes/active', []);

	if (!projectId) {
		const activeRoutes = await activeRoutesReq;
		return { routes: [], certs: [], projectId: '', projects, activeRoutes };
	}

	const [routes, certs, activeRoutes] = await Promise.all([
		safeLoad(() => api.listProxyRoutes(projectId), []),
		safeLoad(() => api.listCertificates(projectId), []),
		activeRoutesReq,
	]);
	const serviceOptions = await safeLoad(
		() => api.metricsServices().then((services) => services.map((s) => s.service_name).sort()),
		[]
	);
	return { routes, certs, projectId, projects, activeRoutes, serviceOptions };
};
