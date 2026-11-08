REVOKE ALL ON SCHEMA routeqtus FROM PUBLIC;
GRANT USAGE ON SCHEMA routeqtus TO routeqtus_serve, routeqtus_manage;
GRANT SELECT ON ALL TABLES IN SCHEMA routeqtus TO routeqtus_serve;
GRANT SELECT, INSERT ON ALL TABLES IN SCHEMA routeqtus TO routeqtus_manage;
GRANT UPDATE ON routeqtus.config_revisions, routeqtus.gateway_state TO routeqtus_manage;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA routeqtus TO routeqtus_manage;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
