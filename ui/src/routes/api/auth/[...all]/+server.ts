import { auth } from '$lib/server/auth';
import { checkRateLimit } from '$lib/server/rate-limit';
import { logger } from '$lib/server/logger';
import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

const handleRequest: RequestHandler = async ({ request, getClientAddress }) => {
	if (request.method === 'POST') {
		const ip = getClientAddress();
		if (!checkRateLimit(`auth:${ip}`)) {
			return json({ error: 'Too many requests' }, { status: 429 });
		}
	}

	try {
		return await auth.handler(request);
	} catch (err) {
		const path = new URL(request.url).pathname;
		logger.error('Auth handler error', {
			method: request.method,
			path,
			error: err instanceof Error ? err.message : String(err),
			stack: err instanceof Error ? err.stack : undefined
		});
		return json(
			{ error: 'Authentication service error. Please try again or check server logs.' },
			{ status: 500 }
		);
	}
};

export const GET = handleRequest;
export const POST = handleRequest;
