import { createApi } from '$lib/api';

export const load = async ({ fetch }: { fetch: typeof globalThis.fetch }) => {
	const api = createApi(fetch);
	const apps = await api.listBespokeApps();
	return { apps };
};
