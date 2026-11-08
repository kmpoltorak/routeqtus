REVOKE ALL ON SCHEMA routetus FROM PUBLIC;
GRANT USAGE ON SCHEMA routetus TO routetus_serve, routetus_manage;
GRANT SELECT ON ALL TABLES IN SCHEMA routetus TO routetus_serve;
GRANT SELECT, INSERT ON ALL TABLES IN SCHEMA routetus TO routetus_manage;
GRANT UPDATE ON routetus.config_revisions, routetus.gateway_state TO routetus_manage;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA routetus TO routetus_manage;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
