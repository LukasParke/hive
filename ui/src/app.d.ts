// See https://svelte.dev/docs/kit/types#app.d.ts
// for information about these interfaces
declare global {
	namespace App {
		interface Error {
			message: string;
			errorId?: string;
		}
		interface Locals {
			user: {
				id: string;
				name: string;
				email: string;
				emailVerified: boolean;
				image?: string | null;
				createdAt: Date;
				updatedAt: Date;
			} | null;
			session: {
				id: string;
				expiresAt: Date;
				token: string;
				createdAt: Date;
				updatedAt: Date;
				ipAddress?: string | null;
				userAgent?: string | null;
				userId: string;
				activeOrg?: string;
			} | null;
		}
	}
}

export {};
