-- SandboxAgent/AgentHarness data carries no rollback guarantee (experimental), so
-- the down migration deletes the kind-qualified rows and their sessions/events
-- outright instead of stripping prefixes back. A 0.9.x binary recreates bare rows
-- for live resources on reconcile; chat history for the experimental kinds does not
-- survive a downgrade.
--
-- Deleting (rather than leaving the rows orphaned) also keeps the up migration
-- re-runnable: a 0.9.x rollback window recreates bare rows for live resources, and
-- qualifying those on re-upgrade would collide with leftover qualified rows.
--
-- The deletes are scoped through the agent table by type + prefix so sessions of an
-- Agent in a namespace literally named "sandboxagents" are never touched.

DELETE FROM event WHERE session_id IN (
  SELECT s.id FROM session s, agent a
  WHERE s.agent_id = a.id
    AND ((a.type = 'SandboxAgent' AND a.id LIKE 'sandboxagents\_\_NS\_\_%')
      OR (a.type = 'AgentHarness' AND a.id LIKE 'agentharnesses\_\_NS\_\_%')));

DELETE FROM session s USING agent a
WHERE s.agent_id = a.id
  AND ((a.type = 'SandboxAgent' AND a.id LIKE 'sandboxagents\_\_NS\_\_%')
    OR (a.type = 'AgentHarness' AND a.id LIKE 'agentharnesses\_\_NS\_\_%'));

DELETE FROM agent
WHERE (type = 'SandboxAgent' AND id LIKE 'sandboxagents\_\_NS\_\_%')
   OR (type = 'AgentHarness' AND id LIKE 'agentharnesses\_\_NS\_\_%');
