import { writable } from 'svelte/store';

interface AuthUser {
	id: string;
	email: string;
	name: string;
	created_at: string;
	updated_at: string;
}

interface AuthSession {
	id: string;
	user_id: string;
	active_org: string;
	expires_at: string;
	created_at: string;
}

interface SessionState {
	data: { user: AuthUser; session: AuthSession } | null;
	isPending: boolean;
}

const sessionStore = writable<SessionState>({ data: null, isPending: true });

let fetchPromise: Promise<void> | null = null;

async function fetchSession() {
	try {
		const res = await fetch('/api/auth/get-session', { credentials: 'include' });
		if (!res.ok) {
			sessionStore.set({ data: null, isPending: false });
			return;
		}
		const body = await res.json();
		if (body.user && body.session) {
			sessionStore.set({ data: { user: body.user, session: body.session }, isPending: false });
		} else {
			sessionStore.set({ data: null, isPending: false });
		}
	} catch {
		sessionStore.set({ data: null, isPending: false });
	}
}

function ensureSessionFetched() {
	if (!fetchPromise) {
		fetchPromise = fetchSession();
	}
	return fetchPromise;
}

export const authClient = {
	useSession: () => {
		ensureSessionFetched();
		return sessionStore;
	},

	signIn: {
		async email({ email, password }: { email: string; password: string }) {
			try {
				const res = await fetch('/api/auth/sign-in/email', {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					credentials: 'include',
					body: JSON.stringify({ email, password }),
				});
				const body = await res.json();
				if (!res.ok) {
					return { error: body.error || { message: 'Login failed' } };
				}
				if (body.user && body.session) {
					sessionStore.set({ data: { user: body.user, session: body.session }, isPending: false });
				}
				return { data: body };
			} catch (e: any) {
				return { error: { message: e.message || 'Login failed' } };
			}
		},
	},

	signUp: {
		async email({ name, email, password }: { name: string; email: string; password: string }) {
			try {
				const res = await fetch('/api/auth/sign-up/email', {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					credentials: 'include',
					body: JSON.stringify({ name, email, password }),
				});
				const body = await res.json();
				if (!res.ok) {
					return { error: body.error || { message: 'Registration failed' } };
				}
				if (body.user && body.session) {
					sessionStore.set({ data: { user: body.user, session: body.session }, isPending: false });
				}
				return { data: body };
			} catch (e: any) {
				return { error: { message: e.message || 'Registration failed' } };
			}
		},
	},

	async signOut() {
		try {
			await fetch('/api/auth/sign-out', {
				method: 'POST',
				credentials: 'include',
			});
		} catch {
			// ignore errors
		}
		sessionStore.set({ data: null, isPending: false });
		fetchPromise = null;
	},
};
