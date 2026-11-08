\getenv serve_password SERVE_DB_PASSWORD
\getenv manage_password MANAGE_DB_PASSWORD
CREATE ROLE routeqtus_serve LOGIN PASSWORD :'serve_password';
CREATE ROLE routeqtus_manage LOGIN PASSWORD :'manage_password';
