-- Kind-qualify the agent DB identity for SandboxAgent and AgentHarness rows so a
-- same-named resource of another kind occupies a distinct row (agent.id is the PK
-- and was previously the bare python identifier of "namespace/name" for all kinds).
-- The prefixes must stay in sync with utils.AgentDBID: "sandboxagents/" and
-- "agentharnesses/" run through ConvertToPythonIdentifier, whose "/" separator is
-- "__NS__". Agent rows keep the bare id; their format is what 0.9.x binaries expect.
--
-- Sessions are rewritten first, while the agent ids in the subselect are still bare.

UPDATE session SET agent_id = 'sandboxagents__NS__' || agent_id
WHERE agent_id IN (SELECT id FROM agent WHERE type = 'SandboxAgent');

UPDATE session SET agent_id = 'agentharnesses__NS__' || agent_id
WHERE agent_id IN (SELECT id FROM agent WHERE type = 'AgentHarness');

UPDATE agent SET id = 'sandboxagents__NS__' || id WHERE type = 'SandboxAgent';

UPDATE agent SET id = 'agentharnesses__NS__' || id WHERE type = 'AgentHarness';
