import { createApi } from '$lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const api = createApi(fetch);
	const [logs, stats] = await Promise.all([
		api.listAuditLogs('limit=100'),
		api.getAuditLogStats(),
	]);
	return { logs, stats };
};
