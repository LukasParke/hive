import { createApi } from '$lib/api';
import type { SwarmNode } from '$lib/types';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const api = createApi(fetch);
	const data = await api.listNodes() as { nodes: SwarmNode[]; join_tokens?: { worker: string; manager: string }; advertise_addr?: string };
	return {
		nodes: data.nodes ?? [],
		joinTokens: data.join_tokens ?? null,
		advertiseAddr: data.advertise_addr ?? null
	};
};
