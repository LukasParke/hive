import { betterAuth } from 'better-auth';
import { prismaAdapter } from 'better-auth/adapters/prisma';
import { prisma } from './db';

function getAuthSecret(): string {
	const secret = process.env.BETTER_AUTH_SECRET;
	if (secret) return secret;
	if (process.env.NODE_ENV === 'production') {
		throw new Error('BETTER_AUTH_SECRET must be set in production');
	}
	return 'hive-dev-secret-do-not-use-in-production';
}

const resolvedBaseURL =
	process.env.BETTER_AUTH_URL || process.env.ORIGIN || process.env.HIVE_URL || 'http://localhost:8080';

function getTrustedOrigins(): string[] {
	const extra = process.env.HIVE_ALLOWED_ORIGINS
		? process.env.HIVE_ALLOWED_ORIGINS.split(',').map((o) => o.trim())
		: [];
	return [resolvedBaseURL, ...extra];
}

export const auth = betterAuth({
	secret: getAuthSecret(),
	baseURL: resolvedBaseURL,
	database: prismaAdapter(prisma, {
		provider: 'postgresql'
	}),
	emailAndPassword: {
		enabled: true,
		minPasswordLength: 8
	},
	session: {
		cookieCache: {
			enabled: true,
			maxAge: 5 * 60
		}
	},
	trustedOrigins: getTrustedOrigins(),
	advanced: {
		ipAddress: {
			ipAddressHeaders: ['x-forwarded-for', 'x-real-ip'],
			trustedProxies: ['10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16', '127.0.0.1/32']
		}
	},
	databaseHooks: {
		user: {
			create: {
				after: async (user) => {
					const existingCount = await prisma.orgRole.count({
						where: { orgId: 'default' }
					});
					await prisma.orgRole.create({
						data: {
							orgId: 'default',
							userId: user.id,
							role: existingCount === 0 ? 'owner' : 'member'
						}
					});
				}
			}
		}
	}
});
