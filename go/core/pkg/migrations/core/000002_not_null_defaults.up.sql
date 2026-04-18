-- Backfill any NULLs (none expected, but safe) then add NOT NULL constraints.
-- These columns always had DEFAULT values but were missing NOT NULL in 000001.

UPDATE feedback SET is_positive = false WHERE is_positive IS NULL;
ALTER TABLE feedback ALTER COLUMN is_positive SET NOT NULL;

UPDATE lg_checkpoint SET version = 1 WHERE version IS NULL;
ALTER TABLE lg_checkpoint ALTER COLUMN version SET NOT NULL;
