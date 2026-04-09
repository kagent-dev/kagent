ALTER TABLE agent ADD COLUMN IF NOT EXISTS workload_type TEXT;
UPDATE agent SET workload_type = 'deployment' WHERE workload_type IS NULL;
ALTER TABLE agent ALTER COLUMN workload_type SET NOT NULL;
