# Azper

Azper is a terminal-native coding-agent harness built on [Pi](https://pi.dev). It keeps Pi's model runtime, tools, skills, extensions, project context, persistent sessions, steering, retries, and compaction while adding a focused interface and a host-side approval boundary.

The visual language is adapted from [brainless](https://brainless.swerdlow.dev): Grok-style sessions, messages, thinking, and permissions; Codex-style command execution; and Claude-style diffs. Brainless itself renders DOM/Tailwind components, so a real terminal cannot import those components directly. Azper implements those patterns with Pi's own `@earendil-works/pi-tui` renderer instead of shipping a browser or drawing a fake terminal in HTML.

## Run

Requirements: Node.js 22+ and an existing Pi login or API-key configuration.

```bash
npm install
npm run build
npm link
azper
```

For development:

```bash
npm run dev
npm run dev -- --cwd /absolute/path/to/project --mode plan
```

Azper continues the most recent Pi session for the workspace by default. Pass `--new` for a clean session.

## Terminal controls

| Input | Action |
| --- | --- |
| `Enter` | Send prompt, or queue a follow-up while Pi is running |
| `Shift+Enter` | Add a newline |
| `Shift+Tab` | Cycle review → plan → auto → full |
| `Ctrl+T` | Cycle the active model's thinking level |
| `Ctrl+K` | Compact context while preserving decisions and evidence |
| `Ctrl+N` | Start a clean session |
| `Ctrl+C` | Abort the active turn; exit when idle |
| `Ctrl+Q` | Exit safely |
| `Ctrl+Shift+F` | Search the transcript |

Slash commands: `/help`, `/mode`, `/models`, `/model`, `/thinking`, `/new`, `/compact`, and `/quit`.

When Azper requests tool approval, press `y` to allow once, `a` to allow that tool for the session, or `n`/`Esc` to reject.

## Modes

| Mode | Tool behavior |
| --- | --- |
| Review | Read-only inspection runs directly; mutations require approval. |
| Plan | Only read, grep, find, and list tools are active. |
| Auto | Ordinary workspace work runs directly; high-impact commands require approval. |
| Full | Workspace tools run without approval prompts. Use only in trusted projects. |

Writes outside the workspace and writes to protected paths such as `.git`, `.env`, `node_modules`, `.ssh`, and Pi's auth file are blocked in every mode. Pi does not provide an OS sandbox, so use a container or VM for untrusted code.

## Harness tuning

- Persistent, workspace-specific sessions with one-at-a-time steering and follow-up queues.
- Automatic context compaction with a 16k-token reserve and 20k recent-token window.
- Three retries with bounded backoff for transient model failures.
- Pi's normal resource loader, so project instructions, skills, prompt files, and extensions still work.
- A concise execution prompt emphasizing acceptance criteria, minimal changes, repository rules, real evidence, proportional verification, and truthful completion reporting.
- A pre-tool extension that enforces modes and pauses the agent while terminal approval is pending.

## Development

```bash
npm run lint
npm test
npm run build
npm run check
```

Core modules:

- `src/harness.ts` owns the Pi runtime, settings, persistence, and pre-tool guardrail.
- `src/policy.ts` classifies tool calls before execution.
- `src/ui/app.ts` owns terminal layout, input, commands, and approval interaction.
- `src/ui/components.ts` renders the brainless-inspired terminal components.
