# Claude Code model-call captures

These are captured with Claude Code `config.PinnedClaudeVersion` (2.1.285). `TestCapturesUseThePinnedClaudeVersion` fails when the pin moves, so recapture on every pin bump.

## Setup

Each scenario runs in a new empty directory `sandbox/` that holds only `notes.txt` with the text `hello\n`. Set `HOME` and `CLAUDE_CONFIG_DIR` to empty temporary directories, so that no user skills, plugins or settings are loaded. Authenticate with `ANTHROPIC_API_KEY`.

```sh
npm install --prefix /tmp/claude-pin @anthropic-ai/claude-code@2.1.285
printf '%s\n' "{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":$PROMPT_JSON}}" |
  /tmp/claude-pin/node_modules/.bin/claude -p --input-format stream-json --output-format stream-json \
    --verbose --include-partial-messages --strict-mcp-config --dangerously-skip-permissions \
    --model "$MODEL" > "claude-$SCENARIO.jsonl"
```

## Scenarios

| Scenario | `MODEL` | Prompt | Note |
|---|---|---|---|
| normal | `claude-opus-5-5` | `Reply with exactly one word: pong` | |
| tools | `claude-opus-5-5` | `Use the Read tool to read notes.txt, then use the Write tool to create copy.txt with the same content, then use the Bash tool to run ls -la. One tool per step. Then summarize in one sentence.` | |
| subagent | `claude-opus-5-5` | `Use the Agent tool to have a subagent read the file notes.txt in the current directory and report its content. Then tell me what it found in one sentence.` | |
| cancel | `claude-opus-5-5` | `Write a 2000-word essay on the history of the printing press.` | Kill the process (SIGKILL) after the 7th `content_block_delta` line. |
| fail | `claude-does-not-exist-9` | `Say hi` | |

## Scrubbing

Make these changes before you commit a capture:

1. Replace the sandbox path with `/work/sandbox`, and the config directory with `/home/user/.claude`. Use the dashed form `-work-sandbox` in project paths.
2. Replace the session ID with `11111111-1111-4111-8111-111111111111`.
3. Replace the user name with `user`, and the socket path with `/tmp/claude.sock`.
4. On the `init` line, set `slash_commands`, `terminal_slash_commands`, `skills`, `plugins` and `agents` to `[]`, and set `memory_paths` to `{}`.
5. Join the `input_json_delta` fragments of each content block into the first delta of that block, and set the other fragments to `""`. Do this so that no part of a local path stays in a fragment.
6. Replace each ID with a placeholder, in order of first appearance in the file:

   | ID | Placeholder |
   |---|---|
   | message ID | `msg_000001`, `msg_000002`, ... |
   | `request_id` | `req_000001`, ... |
   | `agentId` | `agnt000001`, ... |
   | tool use ID (`id`, `tool_use_id`, `parent_tool_use_id`) | `toolu_000001`, ... |
   | line `uuid` | `00000001-0000-4000-8000-000000000000`, ... |

   Use the same placeholder for every occurrence of a value, because the parser deduplicates by message ID. Equal IDs must stay equal, and different IDs must stay different.

After you scrub, make sure that `grep` finds no home or temporary path in the captures. Then update the expected sums in `TestModelCallsFromCaptures`.
