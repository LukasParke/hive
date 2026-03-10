import { createApi } from '$lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const api = createApi(fetch);
	const summary = await api.securitySummary().catch(() => ({
		critical: 0,
		high: 0,
		medium: 0,
		low: 0
	}));
	return { summary };
};
