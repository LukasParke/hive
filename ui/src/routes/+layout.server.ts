import { redirect } from '@sveltejs/kit';
import type { LayoutServerLoad } from './$types';

export const load: LayoutServerLoad = async ({ locals, url }) => {
	const isPublic = url.pathname.startsWith('/auth') || url.pathname === '/healthz';

	if (!isPublic && !locals.user) {
		throw redirect(302, '/auth/login');
	}

	return {
		user: locals.user,
		session: locals.session
	};
};
