DROP INDEX IF EXISTS agent_instance_share_instance_idx;
DROP TABLE IF EXISTS agent_instance_share;
DROP INDEX IF EXISTS agent_instance_namespace_user_id_id_idx;
DROP TABLE IF EXISTS agent_instance;
ALTER TABLE agent_template_harness_pair
    DROP COLUMN IF EXISTS agent_template_labels;
