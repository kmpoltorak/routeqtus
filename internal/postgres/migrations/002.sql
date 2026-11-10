SET LOCAL search_path TO routetus;

-- The upper connection bound is bootstrap limits.max_connections, validated before publication.
ALTER TABLE upstreams
  DROP CONSTRAINT upstreams_max_conns_check,
  DROP CONSTRAINT upstreams_max_idle_conns_check,
  DROP CONSTRAINT upstreams_max_idle_conns_per_host_check,
  ADD CONSTRAINT upstreams_max_conns_check CHECK (max_conns >= 0),
  ADD CONSTRAINT upstreams_max_idle_conns_check CHECK (max_idle_conns >= 0),
  ADD CONSTRAINT upstreams_max_idle_conns_per_host_check CHECK (max_idle_conns_per_host >= 0);
INSERT INTO schema_migrations VALUES (2);
