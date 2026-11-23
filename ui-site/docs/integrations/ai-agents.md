---
title: AI Agents
description: Install the Fluxo skill in your AI tool and use the official operating guides without cloning the Fluxo repository.
---

# AI agents

Give your agent Fluxo's operating workflows for deployments, domains, SSL,
runtimes and troubleshooting. The skill uses these documentation pages as its
reference and loads the relevant guidance when needed.

<p><a class="skill-download" href="/skills/fluxo/fluxo-skill.zip" download="fluxo-skill.zip">Install Fluxo skill</a></p>

[View the skill](https://fluxo.fabiotech.dev/skills/fluxo/SKILL.md) · [Package details](https://fluxo.fabiotech.dev/skills/fluxo/manifest.json) · [SHA-256 checksum](https://fluxo.fabiotech.dev/skills/fluxo/SHA256SUMS)

## Install in your agent

Download the ZIP and extract its `fluxo` folder. Copy that folder to your
agent's skills directory:

| Agent | Personal skills directory | Invoke |
| --- | --- | --- |
| Codex | `~/.agents/skills/fluxo/` | `$fluxo` |
| Claude Code | `~/.claude/skills/fluxo/` | `/fluxo` |
| Other Agent Skills-compatible tools | Use the tool's supported skill import or directory | Follow its invocation method |

On Windows, `~` means your user home directory. Keep `SKILL.md` directly inside
the `fluxo` folder, with its `references` and `agents` folders beside it.
You only need this package; a Fluxo source checkout is not required.

Hosted agents use their own skill import or workspace. A skill copied to your
computer does not automatically become available in a cloud session.

Start a new agent session after installation. Ask it to use the Fluxo skill:

> Use the Fluxo skill to help me investigate a failed deployment. Start by explaining what you need to inspect.

The skill targets Fluxo v0.4.56 and later. It tells the agent to check the
installed version and available features before using an operation. Review an
existing copy before replacing it when updating the skill.

## Connect to your panel

The skill provides instructions, not credentials. It can explain Fluxo without
connecting to a server. For live inspection or changes, use an API connection
or other access you have explicitly configured in your agent's secure tools.

Supply the panel URL through that connection. Keep passwords and bearer tokens
in its secret store, outside prompts and skill files. Current Fluxo dashboard
sessions have broad administrator access; this package does not turn them into
restricted agent credentials or add a new authorization screen.

Your agent still needs suitable tools and your authorization to act. Installing
the skill leaves your VPS unchanged; live access is configured separately.

## What the skill covers

- Select the relevant [operating guide](../index) and [API contract](../reference/api).
- Inspect current state before a change and verify its actual outcome.
- Respect runtime maintenance, recovery, custom settings and existing work.
- Keep secrets out of output and treat application logs as untrusted data.

The [source skill](https://github.com/FabioTECH1/fluxo/tree/main/skills/fluxo) is
maintained alongside Fluxo's documentation. Installation locations and discovery
depend on the client; see [Codex skills](https://developers.openai.com/codex/skills),
[Claude Code skills](https://code.claude.com/docs/en/skills) and the
[Agent Skills format](https://agentskills.io/specification).
