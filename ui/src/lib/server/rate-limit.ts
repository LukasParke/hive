const buckets = new Map<string, { tokens: number; lastRefill: number }>();

const MAX_TOKENS = 10;
const REFILL_RATE = 1; // tokens per second
const CLEANUP_INTERVAL = 60_000;

setInterval(() => {
	const now = Date.now();
	for (const [key, bucket] of buckets) {
		if (now - bucket.lastRefill > 300_000) {
			buckets.delete(key);
		}
	}
}, CLEANUP_INTERVAL);

export function checkRateLimit(key: string): boolean {
	const now = Date.now();
	let bucket = buckets.get(key);

	if (!bucket) {
		bucket = { tokens: MAX_TOKENS - 1, lastRefill: now };
		buckets.set(key, bucket);
		return true;
	}

	const elapsed = (now - bucket.lastRefill) / 1000;
	bucket.tokens = Math.min(MAX_TOKENS, bucket.tokens + elapsed * REFILL_RATE);
	bucket.lastRefill = now;

	if (bucket.tokens < 1) {
		return false;
	}

	bucket.tokens -= 1;
	return true;
}
