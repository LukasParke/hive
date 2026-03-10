import { createApi } from '$lib/api';

export const load = async ({ fetch }: { fetch: typeof globalThis.fetch }) => {
	const api = createApi(fetch);
	const [apps, stacks] = await Promise.all([
		api.listAllApps(),
		api.listAllStacks(),
	]);
	return { apps, stacks };
};
