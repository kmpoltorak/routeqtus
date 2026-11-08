CREATE SCHEMA IF NOT EXISTS routeqtus;
REVOKE ALL ON SCHEMA routeqtus FROM PUBLIC;
SET LOCAL search_path TO routeqtus;

CREATE TABLE schema_migrations (version integer PRIMARY KEY);
CREATE TABLE config_revisions (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  generation bigint UNIQUE CHECK (generation > 0),
  schema_version integer NOT NULL CHECK (schema_version = 1),
  trusted_proxies text[] NOT NULL DEFAULT '{}',
  metrics boolean,
  principal text NOT NULL,
  author text NOT NULL DEFAULT '',
  description text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  sealed boolean NOT NULL DEFAULT false
);
CREATE TABLE gateway_state (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  active_revision bigint REFERENCES config_revisions(id),
  generation bigint NOT NULL DEFAULT 0 CHECK (generation >= 0)
);
INSERT INTO gateway_state(singleton) VALUES (true);
CREATE TABLE upstreams (
  revision_id bigint NOT NULL REFERENCES config_revisions(id),
  id text NOT NULL,
  dial_timeout text, tls_handshake_timeout text, response_header_timeout text,
  idle_timeout text,
  max_conns integer NOT NULL CHECK (max_conns BETWEEN 0 AND 100000),
  max_idle_conns integer NOT NULL CHECK (max_idle_conns BETWEEN 0 AND 100000),
  max_idle_conns_per_host integer NOT NULL CHECK (max_idle_conns_per_host BETWEEN 0 AND 100000),
  proxy_from_environment boolean NOT NULL,
  tls_server_name text NOT NULL, tls_ca_env text NOT NULL,
  tls_cert_env text NOT NULL, tls_key_env text NOT NULL,
  PRIMARY KEY(revision_id,id)
);
CREATE TABLE upstream_targets (
  revision_id bigint NOT NULL,
  upstream_id text NOT NULL,
  id text NOT NULL, url text NOT NULL,
  position integer NOT NULL CHECK (position >= 0),
  PRIMARY KEY(revision_id,upstream_id,id),
  UNIQUE(revision_id,upstream_id,position),
  FOREIGN KEY(revision_id,upstream_id) REFERENCES upstreams(revision_id,id)
);
CREATE TABLE handlers (
  revision_id bigint NOT NULL REFERENCES config_revisions(id),
  id text NOT NULL, type text NOT NULL,
  version integer NOT NULL CHECK (version = 1),
  enabled boolean,
  options jsonb NOT NULL CHECK (jsonb_typeof(options) = 'object'),
  PRIMARY KEY(revision_id,id)
);
CREATE TABLE routes (
  revision_id bigint NOT NULL REFERENCES config_revisions(id),
  id text NOT NULL, host text NOT NULL, methods text[] NOT NULL,
  path text NOT NULL CHECK (left(path,1) = '/'),
  prefix boolean NOT NULL, priority integer NOT NULL,
  strip_prefix boolean NOT NULL CHECK (NOT strip_prefix OR prefix),
  host_header text NOT NULL, upstream_id text NOT NULL,
  request_timeout text, metrics boolean,
  PRIMARY KEY(revision_id,id),
  FOREIGN KEY(revision_id,upstream_id) REFERENCES upstreams(revision_id,id)
);
CREATE TABLE handler_bindings (
  revision_id bigint NOT NULL REFERENCES config_revisions(id),
  route_id text,
  position integer NOT NULL CHECK (position >= 0),
  handler_id text NOT NULL,
  UNIQUE NULLS NOT DISTINCT(revision_id,route_id,position),
  UNIQUE NULLS NOT DISTINCT(revision_id,route_id,handler_id),
  FOREIGN KEY(revision_id,route_id) REFERENCES routes(revision_id,id),
  FOREIGN KEY(revision_id,handler_id) REFERENCES handlers(revision_id,id)
);

CREATE FUNCTION protect_revision() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE rid bigint; published boolean;
BEGIN
  IF TG_TABLE_NAME = 'config_revisions' THEN
    IF OLD.sealed THEN RAISE EXCEPTION 'published revision is immutable'; END IF;
    IF NEW.sealed AND NEW.generation IS NULL THEN RAISE EXCEPTION 'published revision requires generation'; END IF;
  ELSE
    IF TG_OP = 'DELETE' THEN rid := OLD.revision_id; ELSE rid := NEW.revision_id; END IF;
    SELECT sealed INTO published FROM config_revisions WHERE id = rid FOR SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION 'revision is not visible'; END IF;
    IF published THEN
      RAISE EXCEPTION 'published revision is immutable';
    END IF;
    IF TG_OP = 'UPDATE' AND OLD.revision_id <> NEW.revision_id THEN
      RAISE EXCEPTION 'entities cannot move between revisions';
    END IF;
  END IF;
  IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER immutable_revision BEFORE UPDATE OR DELETE ON config_revisions FOR EACH ROW EXECUTE FUNCTION protect_revision();
CREATE TRIGGER immutable_upstream BEFORE INSERT OR UPDATE OR DELETE ON upstreams FOR EACH ROW EXECUTE FUNCTION protect_revision();
CREATE TRIGGER immutable_target BEFORE INSERT OR UPDATE OR DELETE ON upstream_targets FOR EACH ROW EXECUTE FUNCTION protect_revision();
CREATE TRIGGER immutable_handler BEFORE INSERT OR UPDATE OR DELETE ON handlers FOR EACH ROW EXECUTE FUNCTION protect_revision();
CREATE TRIGGER immutable_route BEFORE INSERT OR UPDATE OR DELETE ON routes FOR EACH ROW EXECUTE FUNCTION protect_revision();
CREATE TRIGGER immutable_binding BEFORE INSERT OR UPDATE OR DELETE ON handler_bindings FOR EACH ROW EXECUTE FUNCTION protect_revision();
CREATE FUNCTION protect_publication() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.generation <> OLD.generation + 1 THEN RAISE EXCEPTION 'generation must increase by one'; END IF;
  IF NOT EXISTS(SELECT 1 FROM config_revisions WHERE id=NEW.active_revision AND sealed AND generation=NEW.generation) THEN
    RAISE EXCEPTION 'active revision must be sealed';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER valid_publication BEFORE UPDATE ON gateway_state FOR EACH ROW EXECUTE FUNCTION protect_publication();
INSERT INTO schema_migrations VALUES (1);
