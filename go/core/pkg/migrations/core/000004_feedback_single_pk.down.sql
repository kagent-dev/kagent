-- No-op: 000004 is a corrective convergence step. Both fresh installs (which
-- already had PRIMARY KEY (id) from 000001) and upgraded installs (which
-- carried the legacy composite PRIMARY KEY (id, user_id) from GORM
-- AutoMigrate) end at PRIMARY KEY (id). Once up has run, there is no way to
-- tell which path the database came from, so a symmetric down would put fresh
-- installs into a composite-PK state they never originally had. Leaving the
-- single-column PK in place on rollback is safer and the application is
-- PK-shape independent (queries do not reference the PK directly).
SELECT 1;
