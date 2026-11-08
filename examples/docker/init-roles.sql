\getenv serve_password SERVE_DB_PASSWORD
\getenv manage_password MANAGE_DB_PASSWORD
CREATE ROLE routetus_serve LOGIN PASSWORD :'serve_password';
CREATE ROLE routetus_manage LOGIN PASSWORD :'manage_password';
