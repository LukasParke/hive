import { betterAuth } from 'better-auth';
import { organization } from 'better-auth/plugins';

export const auth = betterAuth({
	database: {
		type: 'postgres',
		url: process.env.DATABASE_URL || 'postgres://hive:hive-secret@hive-postgres:5432/hive'
	},
	plugins: [
		organization()
	],
	session: {
		cookieCache: {
			enabled: true,
			maxAge: 5 * 60
		}
	},
	emailAndPassword: {
		enabled: true
	}
});
