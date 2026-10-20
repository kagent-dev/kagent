-- Copyright 2026 The Kagent Authors.
--
-- Licensed under the Apache License, Version 2.0 (the "License");
-- you may not use this file except in compliance with the License.
-- You may obtain a copy of the License at
--
--     http://www.apache.org/licenses/LICENSE-2.0
--
-- Unless required by applicable law or agreed to in writing, software
-- distributed under the License is distributed on an "AS IS" BASIS,
-- WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
-- See the License for the specific language governing permissions and
-- limitations under the License.

-- Creates Kagent's owner and read/write roles, their login users, and the
-- schema. Run it with psql as a database administrator, in one transaction,
-- with every variable set:
--
--   psql --no-psqlrc --set=ON_ERROR_STOP=1 --single-transaction \
--     --set=kagent_schema=kagent \
--     --set=kagent_owner_role=kagent_owner \
--     --set=kagent_owner_user=kagent_owner_user \
--     --set=kagent_owner_password= \
--     --set=kagent_readwrite_role=kagent_readwrite \
--     --set=kagent_readwrite_user=kagent_readwrite_user \
--     --set=kagent_readwrite_password= \
--     --file=setup.sql
--
-- The owner role owns the schema and every object migrations create. The
-- read/write role can read and write rows but not change the schema. Each
-- login's password is set to its variable on every run; an empty value removes
-- it, for logins that authenticate with certificates.

-- \gset keeps the passwords out of psql's output.
SELECT
    set_config('kagent.setup_schema', :'kagent_schema', true) AS schema,
    set_config('kagent.setup_owner_role', :'kagent_owner_role', true) AS owner_role,
    set_config('kagent.setup_owner_user', :'kagent_owner_user', true) AS owner_user,
    set_config('kagent.setup_owner_password', :'kagent_owner_password', true) AS owner_password,
    set_config('kagent.setup_readwrite_role', :'kagent_readwrite_role', true) AS readwrite_role,
    set_config('kagent.setup_readwrite_user', :'kagent_readwrite_user', true) AS readwrite_user,
    set_config('kagent.setup_readwrite_password', :'kagent_readwrite_password', true) AS readwrite_password
\gset

DO $setup$
DECLARE
    schema_name text := current_setting('kagent.setup_schema');
    owner_role text := current_setting('kagent.setup_owner_role');
    owner_user text := current_setting('kagent.setup_owner_user');
    owner_password text := NULLIF(current_setting('kagent.setup_owner_password'), '');
    readwrite_role text := current_setting('kagent.setup_readwrite_role');
    readwrite_user text := current_setting('kagent.setup_readwrite_user');
    readwrite_password text := NULLIF(current_setting('kagent.setup_readwrite_password'), '');
    managed record;
    role_attrs record;
    schema_owner text;
BEGIN
    -- Outside a transaction, psql's autocommit discards the settings above.
    IF schema_name = '' OR owner_role = '' OR owner_user = '' OR readwrite_role = '' OR readwrite_user = '' THEN
        RAISE EXCEPTION 'set kagent_schema and the kagent_owner_* and kagent_readwrite_* role and user variables, and run the script in one transaction';
    END IF;
    -- A shared name would merge the owner's and the read/write login's privileges.
    IF (SELECT count(DISTINCT name) FROM unnest(ARRAY[owner_role, owner_user, readwrite_role, readwrite_user]) AS name) <> 4 THEN
        RAISE EXCEPTION 'the owner and read/write role and user names must all be different';
    END IF;

    PERFORM pg_advisory_xact_lock(hashtextextended('kagent:setup:' || schema_name, 0));

    FOR managed IN SELECT * FROM (VALUES
        (owner_role, false, NULL::text),
        (readwrite_role, false, NULL::text),
        (owner_user, true, owner_password),
        (readwrite_user, true, readwrite_password)
    ) AS roles(name, can_login, password) LOOP
        SELECT rolcanlogin, rolsuper, rolcreatedb, rolcreaterole, rolreplication, rolinherit
          INTO role_attrs FROM pg_roles WHERE rolname = managed.name;
        IF NOT FOUND THEN
            IF managed.can_login THEN
                EXECUTE format('CREATE ROLE %I LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION', managed.name);
            ELSE
                EXECUTE format('CREATE ROLE %I NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION', managed.name);
            END IF;
        ELSIF role_attrs.rolcanlogin <> managed.can_login OR role_attrs.rolsuper
           OR role_attrs.rolcreatedb OR role_attrs.rolcreaterole OR role_attrs.rolreplication
           OR (managed.can_login AND role_attrs.rolinherit) THEN
            RAISE EXCEPTION 'managed PostgreSQL role "%" conflicts with the required attributes', managed.name;
        END IF;
        IF managed.can_login THEN
            EXECUTE format('ALTER ROLE %I PASSWORD %L', managed.name, managed.password);
        END IF;
    END LOOP;

    EXECUTE format('GRANT %I TO %I', owner_role, owner_user);
    EXECUTE format('GRANT %I TO %I', readwrite_role, readwrite_user);

    SELECT pg_get_userbyid(nspowner) INTO schema_owner FROM pg_namespace WHERE nspname = schema_name;
    IF NOT FOUND THEN
        EXECUTE format('CREATE SCHEMA %I AUTHORIZATION %I', schema_name, owner_role);
    ELSIF schema_owner <> owner_role THEN
        RAISE EXCEPTION 'PostgreSQL schema "%" is owned by "%", not "%"', schema_name, schema_owner, owner_role;
    END IF;

    REVOKE CREATE ON SCHEMA public FROM PUBLIC;
    EXECUTE format('REVOKE ALL ON SCHEMA %I FROM PUBLIC', schema_name);
    EXECUTE format('GRANT USAGE ON SCHEMA %I TO %I', schema_name, readwrite_role);
    EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA %I GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %I', owner_role, schema_name, readwrite_role);
    EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA %I GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO %I', owner_role, schema_name, readwrite_role);
    EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA %I GRANT EXECUTE ON ROUTINES TO %I', owner_role, schema_name, readwrite_role);
    EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA %I GRANT USAGE ON TYPES TO %I', owner_role, schema_name, readwrite_role);
END
$setup$;
