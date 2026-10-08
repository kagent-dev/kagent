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

DO $setup$
DECLARE
    managed record;
    role_attrs record;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('kagent-install:database-setup', 0));

    FOR managed IN SELECT * FROM (VALUES
        ('kagent_owner', false, NULL::text),
        ('kagent_user', true, NULL::text)
    ) AS roles(name, can_login, password) LOOP
        SELECT rolcanlogin, rolsuper, rolcreatedb, rolcreaterole, rolreplication, rolinherit
          INTO role_attrs FROM pg_roles WHERE rolname = managed.name;
        IF NOT FOUND THEN
            IF managed.can_login THEN
                EXECUTE format('CREATE ROLE %I LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD %L', managed.name, managed.password);
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

    GRANT kagent_owner TO kagent_user;

    -- Kagent's tables live in public. Only its role may create objects there.
    REVOKE CREATE ON SCHEMA public FROM PUBLIC;
    GRANT USAGE, CREATE ON SCHEMA public TO kagent_owner;
END
$setup$;
