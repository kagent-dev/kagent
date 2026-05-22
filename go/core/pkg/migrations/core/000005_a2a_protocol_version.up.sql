ALTER TABLE task ADD COLUMN IF NOT EXISTS protocol_version TEXT;
ALTER TABLE push_notification ADD COLUMN IF NOT EXISTS protocol_version TEXT;
