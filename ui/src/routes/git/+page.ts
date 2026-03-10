import { createApi } from '$lib/api';

export const load = async ({ fetch }: { fetch: typeof globalThis.fetch }) => {
	const api = createApi(fetch);
	const [sources, ghStatus] = await Promise.all([
		api.listGitSources(),
		api.githubAppStatus().catch(() => ({
			configured: false, slug: '', installed: false,
			installation_id: undefined as number | undefined,
			html_url: undefined as string | undefined,
			app_id: undefined as number | undefined,
		})),
	]);
	return { sources, ghStatus };
};
