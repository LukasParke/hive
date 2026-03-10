import { PrismaClient } from '@prisma/client';
import { PrismaPg } from '@prisma/adapter-pg';
import pg from 'pg';

function createPrismaClient(): PrismaClient {
	const connectionString = process.env.DATABASE_URL;
	if (!connectionString) {
		console.warn('[hive] DATABASE_URL not set, Prisma client will not be available');
		return new Proxy({} as PrismaClient, {
			get(_, prop) {
				if (prop === 'then') return undefined;
				throw new Error('DATABASE_URL is not configured');
			}
		});
	}

	const pool = new pg.Pool({
		connectionString,
		max: 20,
		idleTimeoutMillis: 30_000,
		connectionTimeoutMillis: 10_000
	});

	const adapter = new PrismaPg(pool);
	return new PrismaClient({ adapter });
}

const globalForPrisma = globalThis as unknown as { prisma: PrismaClient };

export const prisma = globalForPrisma.prisma || createPrismaClient();

if (process.env.NODE_ENV !== 'production') {
	globalForPrisma.prisma = prisma;
}
