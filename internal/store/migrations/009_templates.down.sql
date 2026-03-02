DROP TABLE IF EXISTS custom_template;
DROP TABLE IF EXISTS template_source;
ALTER TABLE app DROP COLUMN IF EXISTS template_name;
ALTER TABLE app DROP COLUMN IF EXISTS template_version;
