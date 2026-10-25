-- +goose Up
ALTER TABLE agent_definition
    ADD COLUMN grant_token_passthrough_targets JSONB NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(grant_token_passthrough_targets) = 'array'),
    ADD COLUMN integration_grants JSONB NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(integration_grants) = 'array');
ALTER TABLE sandbox_template_definition
    ADD COLUMN grant_token_passthrough_targets JSONB NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(grant_token_passthrough_targets) = 'array');

-- +goose Down
ALTER TABLE sandbox_template_definition DROP COLUMN grant_token_passthrough_targets;
ALTER TABLE agent_definition
    DROP COLUMN integration_grants,
    DROP COLUMN grant_token_passthrough_targets;
