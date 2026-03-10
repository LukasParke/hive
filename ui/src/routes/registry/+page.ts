import { createApi } from '$lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const api = createApi(fetch);
	const [status, images] = await Promise.all([
		api.registryStatus(),
		api.registryImages().catch(() => []),
	]);
	return { status, images };
};
