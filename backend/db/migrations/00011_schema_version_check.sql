-- +goose Up
-- Lets running processes check that the database is not behind the code before serving.
GRANT SELECT ON public.goose_db_version TO msp_app, msp_system;

-- +goose Down
REVOKE SELECT ON public.goose_db_version FROM msp_app, msp_system;
