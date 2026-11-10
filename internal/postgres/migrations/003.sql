SET LOCAL search_path TO routetus;

-- Optional metadata keeps old revisions unchanged when exported.
ALTER TABLE upstreams ADD COLUMN service_id text NOT NULL DEFAULT '';
ALTER TABLE routes ADD COLUMN service_id text NOT NULL DEFAULT '';
INSERT INTO schema_migrations VALUES (3);
