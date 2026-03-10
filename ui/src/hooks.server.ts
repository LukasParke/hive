import { auth } from '$lib/server/auth';
import { svelteKitHandler } from 'better-auth/svelte-kit';
import { building } from '$app/environment';
import { logger, generateErrorId } from '$lib/server/logger';
import type { Handle, HandleServerError } from '@sveltejs/kit';

const PUBLIC_PATHS = ['/healthz'];

export const handle: Handle = async ({ event, resolve }) => {
	if (PUBLIC_PATHS.includes(event.url.pathname)) {
		return resolve(event);
	}

	try {
		const session = await auth.api.getSession({
			headers: event.request.headers
		});

		if (session) {
			event.locals.session = session.session;
			event.locals.user = session.user;
		}
	} catch (err) {
		logger.error('Session resolution failed', {
			path: event.url.pathname,
			error: err instanceof Error ? err.message : String(err)
		});
	}

	return svelteKitHandler({ event, resolve, auth, building });
};

export const handleError: HandleServerError = async ({ error, event, status, message }) => {
	const errorId = generateErrorId();

	logger.error('Unhandled server error', {
		errorId,
		status,
		method: event.request.method,
		path: event.url.pathname,
		message: error instanceof Error ? error.message : String(error),
		stack: error instanceof Error ? error.stack : undefined
	});

	return {
		message: process.env.NODE_ENV === 'production'
			? `Something went wrong (ref: ${errorId})`
			: message || 'Internal server error',
		errorId
	};
};
