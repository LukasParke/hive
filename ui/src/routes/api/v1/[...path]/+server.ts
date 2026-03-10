import type { RequestHandler } from '@sveltejs/kit';

const ENGINE_URL = process.env.HIVE_ENGINE_URL || 'http://localhost:9090';

const proxy: RequestHandler = async ({ params, request, url }) => {
	const target = `${ENGINE_URL}/api/v1/${params.path}${url.search}`;

	const headers = new Headers(request.headers);
	headers.delete('host');

	const hasBody = !['GET', 'HEAD'].includes(request.method);

	const resp = await fetch(target, {
		method: request.method,
		headers,
		body: hasBody ? request.body : undefined,
		// @ts-expect-error Node.js fetch requires duplex for streaming bodies
		duplex: hasBody ? 'half' : undefined,
	});

	const respHeaders = new Headers(resp.headers);
	respHeaders.delete('transfer-encoding');

	return new Response(resp.body, {
		status: resp.status,
		statusText: resp.statusText,
		headers: respHeaders,
	});
};

export const GET = proxy;
export const POST = proxy;
export const PUT = proxy;
export const PATCH = proxy;
export const DELETE = proxy;
export const OPTIONS = proxy;
