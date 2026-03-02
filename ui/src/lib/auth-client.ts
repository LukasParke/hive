import { createAuthClient } from 'better-auth/svelte';
import { organizationClient } from 'better-auth/client/plugins';

export const authClient = createAuthClient({
	baseURL: typeof window !== 'undefined' ? window.location.origin : 'http://localhost:3000',
	plugins: [
		organizationClient()
	]
});
