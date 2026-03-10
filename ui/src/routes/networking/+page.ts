import { createApi } from '$lib/api';
import { safeLoad } from '$lib/load-utils';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const api = createApi(fetch);
	const [connectivity, settings] = await Promise.all([
		safeLoad(() => api.checkConnectivity(), null),
		safeLoad(() => api.getNetworkingSettings(), {}),
	]);
	return { connectivity, settings };
};
