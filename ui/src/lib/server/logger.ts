type LogLevel = 'debug' | 'info' | 'warn' | 'error';

interface LogContext {
	[key: string]: unknown;
}

const LEVEL_PRIORITY: Record<LogLevel, number> = {
	debug: 0,
	info: 1,
	warn: 2,
	error: 3
};

const MIN_LEVEL: LogLevel = (process.env.LOG_LEVEL as LogLevel) || 'info';

function shouldLog(level: LogLevel): boolean {
	return LEVEL_PRIORITY[level] >= LEVEL_PRIORITY[MIN_LEVEL];
}

function formatEntry(level: LogLevel, message: string, context?: LogContext): string {
	const entry: Record<string, unknown> = {
		ts: new Date().toISOString(),
		level,
		msg: message
	};
	if (context) {
		for (const [k, v] of Object.entries(context)) {
			entry[k] = v;
		}
	}
	return JSON.stringify(entry);
}

function log(level: LogLevel, message: string, context?: LogContext): void {
	if (!shouldLog(level)) return;
	const line = formatEntry(level, message, context);
	if (level === 'error') {
		console.error(line);
	} else if (level === 'warn') {
		console.warn(line);
	} else {
		console.log(line);
	}
}

export const logger = {
	debug: (msg: string, ctx?: LogContext) => log('debug', msg, ctx),
	info: (msg: string, ctx?: LogContext) => log('info', msg, ctx),
	warn: (msg: string, ctx?: LogContext) => log('warn', msg, ctx),
	error: (msg: string, ctx?: LogContext) => log('error', msg, ctx)
};

let counter = 0;
export function generateErrorId(): string {
	counter = (counter + 1) % 0xffff;
	const ts = Date.now().toString(36);
	const seq = counter.toString(36).padStart(3, '0');
	return `E-${ts}-${seq}`;
}
