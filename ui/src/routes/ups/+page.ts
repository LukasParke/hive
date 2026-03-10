import type { PageLoad } from './$types';
import { createApi } from '$lib/api';

export const load: PageLoad = async ({ fetch }) => {
	const api = createApi(fetch);
	return { data: await api.listUPS().catch(() => []) };
};
