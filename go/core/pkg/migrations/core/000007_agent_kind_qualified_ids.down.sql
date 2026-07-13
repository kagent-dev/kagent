-- Strip the kind prefixes added by the up migration. Sessions are reverted first,
-- scoped through the agent table while its ids are still qualified, so a bare id
-- that happens to start with the same text (an Agent in a namespace literally
-- named "sandboxagents") is never touched.
--
-- Ghost dedup: a 0.9.x binary run against the migrated schema (compatibility mode)
-- re-upserts BARE rows for live SandboxAgent/AgentHarness resources — duplicates of
-- the qualified rows for the same resource. Those same-kind ghosts are deleted here
-- before stripping, so the qualified row (which pre-rollback sessions point at)
-- survives and rollback-window sessions with bare agent_id re-attach to it.
--
-- Rollback limit: if an *Agent* and a SandboxAgent/AgentHarness legitimately share a
-- namespace/name (the feature this migration enables), the bare row is a different
-- resource — never deleted — and stripping the prefix collides with its primary key,
-- aborting with a unique violation. Delete one of the colliding resources (and its
-- sessions) before rolling back.

UPDATE session SET agent_id = substring(agent_id FROM char_length('sandboxagents__NS__') + 1)
WHERE agent_id IN (SELECT id FROM agent WHERE type = 'SandboxAgent' AND id LIKE 'sandboxagents\_\_NS\_\_%');

UPDATE session SET agent_id = substring(agent_id FROM char_length('agentharnesses__NS__') + 1)
WHERE agent_id IN (SELECT id FROM agent WHERE type = 'AgentHarness' AND id LIKE 'agentharnesses\_\_NS\_\_%');

-- Collisions prefer live rows over soft-deleted ones: a soft-deleted qualified row
-- whose stripped id already exists loses to the existing bare row (whatever its
-- kind); a live qualified row wins over its same-kind bare ghost. Only two LIVE
-- rows of different kinds reach the strip and abort.

DELETE FROM agent q
WHERE q.deleted_at IS NOT NULL
  AND q.type = 'SandboxAgent' AND q.id LIKE 'sandboxagents\_\_NS\_\_%'
  AND EXISTS (SELECT 1 FROM agent bare WHERE bare.id = substring(q.id FROM char_length('sandboxagents__NS__') + 1));

DELETE FROM agent q
WHERE q.deleted_at IS NOT NULL
  AND q.type = 'AgentHarness' AND q.id LIKE 'agentharnesses\_\_NS\_\_%'
  AND EXISTS (SELECT 1 FROM agent bare WHERE bare.id = substring(q.id FROM char_length('agentharnesses__NS__') + 1));

DELETE FROM agent bare
WHERE bare.type = 'SandboxAgent'
  AND bare.id NOT LIKE 'sandboxagents\_\_NS\_\_%'
  AND EXISTS (SELECT 1 FROM agent q WHERE q.type = 'SandboxAgent' AND q.deleted_at IS NULL AND q.id = 'sandboxagents__NS__' || bare.id);

DELETE FROM agent bare
WHERE bare.type = 'AgentHarness'
  AND bare.id NOT LIKE 'agentharnesses\_\_NS\_\_%'
  AND EXISTS (SELECT 1 FROM agent q WHERE q.type = 'AgentHarness' AND q.deleted_at IS NULL AND q.id = 'agentharnesses__NS__' || bare.id);

-- A soft-deleted bare row of ANY kind loses to a live qualified row taking its id
-- (e.g. an Agent was soft-deleted and a same-named SandboxAgent created afterwards).
DELETE FROM agent bare
WHERE bare.deleted_at IS NOT NULL
  AND bare.id NOT LIKE 'sandboxagents\_\_NS\_\_%' AND bare.id NOT LIKE 'agentharnesses\_\_NS\_\_%'
  AND EXISTS (
    SELECT 1 FROM agent q
    WHERE q.deleted_at IS NULL
      AND (q.id = 'sandboxagents__NS__' || bare.id OR q.id = 'agentharnesses__NS__' || bare.id)
  );

UPDATE agent SET id = substring(id FROM char_length('sandboxagents__NS__') + 1)
WHERE type = 'SandboxAgent' AND id LIKE 'sandboxagents\_\_NS\_\_%';

UPDATE agent SET id = substring(id FROM char_length('agentharnesses__NS__') + 1)
WHERE type = 'AgentHarness' AND id LIKE 'agentharnesses\_\_NS\_\_%';
