import * as fs from 'node:fs';
import * as path from 'node:path';
import yaml from 'js-yaml';

export interface BuiltinTemplate {
	kind: string;
	name: string;
	description: string;
	category: string;
	icon: string;
	image: string;
	ports: string[];
	volumes: string[];
	env: Record<string, string>;
	domain: string;
	replicas: number;
	depends_on?: { type: string; version: string }[];
	homepage_labels?: Record<string, string>;
	traefik_labels?: Record<string, string>;
	nas_volumes?: { name: string; suggested_path: string; description: string }[];
	stack?: boolean;
	services?: { name: string; image: string; ports?: string[]; env?: Record<string, string>; volumes?: string[] }[];
}

let cached: BuiltinTemplate[] | null = null;

function resolveTemplatesDir(): string {
	// In Docker: /app/templates/
	// In dev: ../../templates/ relative to ui/
	const dockerPath = '/app/templates';
	if (fs.existsSync(dockerPath)) return dockerPath;

	const devPath = path.resolve(process.cwd(), '..', 'templates');
	if (fs.existsSync(devPath)) return devPath;

	const altDevPath = path.resolve(process.cwd(), 'templates');
	if (fs.existsSync(altDevPath)) return altDevPath;

	return dockerPath;
}

export function loadBuiltinTemplates(): BuiltinTemplate[] {
	if (cached) return cached;

	const dir = resolveTemplatesDir();
	const templates: BuiltinTemplate[] = [];

	let entries: fs.Dirent[];
	try {
		entries = fs.readdirSync(dir, { withFileTypes: true });
	} catch {
		cached = [];
		return [];
	}

	for (const entry of entries) {
		if (entry.isDirectory()) continue;
		const ext = path.extname(entry.name);
		if (ext !== '.yaml' && ext !== '.yml') continue;

		try {
			const content = fs.readFileSync(path.join(dir, entry.name), 'utf-8');
			const tmpl = yaml.load(content) as BuiltinTemplate;
			if (!tmpl?.name) continue;

			tmpl.ports ??= [];
			tmpl.volumes ??= [];
			tmpl.env ??= {};
			tmpl.domain ??= '';
			tmpl.replicas = tmpl.replicas || 1;
			tmpl.category ??= 'other';
			tmpl.description ??= '';
			tmpl.icon ??= '';
			tmpl.image ??= '';

			templates.push(tmpl);
		} catch {
			// Skip malformed templates
		}
	}

	templates.sort((a, b) => a.name.localeCompare(b.name));
	cached = templates;
	return templates;
}

export function getBuiltinTemplate(name: string): BuiltinTemplate | undefined {
	return loadBuiltinTemplates().find((t) => t.name === name);
}
