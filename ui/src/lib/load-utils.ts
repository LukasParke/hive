export async function safeLoad<T>(fn: () => Promise<T>, fallback: T): Promise<T> {
	try {
		return await fn();
	} catch {
		return fallback;
	}
}

export async function safeJSONFetch<T>(fetchFn: typeof fetch, url: string, fallback: T): Promise<T> {
	try {
		const res = await fetchFn(url, { credentials: 'include' });
		if (!res.ok) return fallback;
		return (await res.json()) as T;
	} catch {
		return fallback;
	}
}
