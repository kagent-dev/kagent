-- Strip the kind prefixes added by the up migration. Sessions are reverted first,
-- scoped through the agent table while its ids are still qualified, so a bare id
-- that happens to start with the same text (an Agent in a namespace literally
-- named "sandboxagents") is never touched.
--
-- Rollback limit: if an Agent and a SandboxAgent/AgentHarness legitimately share a
-- namespace/name (the feature this migration enables), stripping the prefix would
-- collide with the Agent row's primary key and this migration aborts with a unique
-- violation. Delete one of the colliding resources (and its sessions) before
-- rolling back.

UPDATE session SET agent_id = substring(agent_id FROM char_length('sandboxagents__NS__') + 1)
WHERE agent_id IN (SELECT id FROM agent WHERE type = 'SandboxAgent' AND id LIKE 'sandboxagents\_\_NS\_\_%');

UPDATE session SET agent_id = substring(agent_id FROM char_length('agentharnesses__NS__') + 1)
WHERE agent_id IN (SELECT id FROM agent WHERE type = 'AgentHarness' AND id LIKE 'agentharnesses\_\_NS\_\_%');

UPDATE agent SET id = substring(id FROM char_length('sandboxagents__NS__') + 1)
WHERE type = 'SandboxAgent' AND id LIKE 'sandboxagents\_\_NS\_\_%';

UPDATE agent SET id = substring(id FROM char_length('agentharnesses__NS__') + 1)
WHERE type = 'AgentHarness' AND id LIKE 'agentharnesses\_\_NS\_\_%';
