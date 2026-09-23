-- PostgreSQL identity setup for Kagent. This is separate from table migrations.
-- Run as an administrator inside a transaction after setting these transaction-local
-- settings: kagent.bootstrap_username, kagent.bootstrap_password,
-- kagent.bootstrap_schema, kagent.bootstrap_vector_enabled, and optionally
-- kagent.bootstrap_vector_schema (default extensions). Optionally set
-- kagent.bootstrap_owner_role (default kagent_owner) for a manually
-- provisioned install.
-- The bundled bootstrap supplies its fixed development credentials and schema.
-- Operators may supply their own values when running this file directly.

DO $bootstrap$
DECLARE
    app_user text := current_setting('kagent.bootstrap_username');
    app_password text := current_setting('kagent.bootstrap_password');
    schema_name text := current_setting('kagent.bootstrap_schema');
    owner_role text := COALESCE(NULLIF(current_setting('kagent.bootstrap_owner_role', true), ''), 'kagent_owner');
    vector_enabled boolean := current_setting('kagent.bootstrap_vector_enabled')::boolean;
    vector_schema text := COALESCE(NULLIF(current_setting('kagent.bootstrap_vector_schema', true), ''), 'extensions');
    installed_vector_schema text;
    role_attrs record;
    schema_owner text;
BEGIN
    IF app_user = '' OR app_password = '' OR schema_name = '' THEN
        RAISE EXCEPTION 'Kagent bootstrap username, password, and schema must not be empty';
    END IF;
    IF owner_role = app_user THEN
        RAISE EXCEPTION 'Kagent owner role and login username must differ';
    END IF;
    PERFORM pg_advisory_xact_lock(hashtextextended('kagent:bootstrap:' || schema_name, 0));

    SELECT rolcanlogin, rolsuper, rolcreatedb, rolcreaterole, rolreplication, rolinherit
      INTO role_attrs FROM pg_roles WHERE rolname = owner_role;
    IF NOT FOUND THEN
        EXECUTE format('CREATE ROLE %I NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION', owner_role);
    ELSIF role_attrs.rolcanlogin OR role_attrs.rolsuper OR role_attrs.rolcreatedb
       OR role_attrs.rolcreaterole OR role_attrs.rolreplication THEN
        RAISE EXCEPTION 'managed PostgreSQL role "%" conflicts with the required attributes', owner_role;
    END IF;

    SELECT rolcanlogin, rolsuper, rolcreatedb, rolcreaterole, rolreplication, rolinherit
      INTO role_attrs FROM pg_roles WHERE rolname = app_user;
    IF NOT FOUND THEN
        EXECUTE format('CREATE ROLE %I LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD %L', app_user, app_password);
    ELSIF NOT role_attrs.rolcanlogin OR role_attrs.rolsuper OR role_attrs.rolcreatedb
       OR role_attrs.rolcreaterole OR role_attrs.rolreplication OR role_attrs.rolinherit THEN
        RAISE EXCEPTION 'managed PostgreSQL role "%" conflicts with the required attributes', app_user;
    END IF;

    EXECUTE format('GRANT %I TO %I', owner_role, app_user);
    SELECT pg_get_userbyid(nspowner) INTO schema_owner FROM pg_namespace WHERE nspname = schema_name;
    IF NOT FOUND THEN
        EXECUTE format('CREATE SCHEMA %I AUTHORIZATION %I', schema_name, owner_role);
    ELSIF schema_name = 'public' THEN
        EXECUTE format('GRANT USAGE, CREATE ON SCHEMA public TO %I', owner_role);
    ELSIF schema_owner <> owner_role THEN
        RAISE EXCEPTION 'PostgreSQL schema "%" is owned by "%", not "%"', schema_name, schema_owner, owner_role;
    END IF;

    REVOKE CREATE ON SCHEMA public FROM PUBLIC;
    EXECUTE format('REVOKE ALL ON SCHEMA %I FROM PUBLIC', schema_name);
    IF vector_enabled THEN
        SELECT n.nspname INTO installed_vector_schema
          FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace
         WHERE e.extname = 'vector';
        IF FOUND AND installed_vector_schema <> vector_schema THEN
            RAISE EXCEPTION 'pgvector is installed in schema "%", expected "%"', installed_vector_schema, vector_schema;
        END IF;
        IF NOT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = vector_schema) THEN
            EXECUTE format('CREATE SCHEMA %I', vector_schema);
        END IF;
        EXECUTE format('GRANT USAGE ON SCHEMA %I TO %I', vector_schema, owner_role);
        EXECUTE format('CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA %I', vector_schema);
    END IF;
END
$bootstrap$;
