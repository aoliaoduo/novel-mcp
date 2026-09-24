# User workflow

[简体中文](WORKFLOW.zh-CN.md) · **English**

This document describes how an MCP client should drive `novel-mcp`. It is about protocol behavior, not literary style.

## Mental model

`novel-mcp` is a durable artifact/state service. The client AI is responsible for semantic judgment and prose. The server is responsible for facts, validation, persistence, routing, concurrency protection, and recovery.

The control loop is:

```text
next_step → execute the returned actions → next_step → ... → done=true
```

Do not implement a second workflow engine in the client. Ask `next_step` again after finishing the current plan.

## Starting or opening a project

A client normally begins with:

```text
list_projects
```

Then either select an existing project or call `create_project` with a safe project ID and a useful brief.

`create_project` is the one creation call that does not use an existing project revision. Once a project exists, mutations require the latest `expected_revision` returned by that project.

## First planning is intentionally staged

When a new project has no planning tier yet, `next_step` returns only enough actions to establish it:

1. Read `novel_guide` with `role=architect` or `role=architect_long` based on the brief.
2. Save book metadata if it is missing.
3. Save a seed premise with `scale=short|mid|long`.
4. Call `next_step` again.

The second call now has a persisted tier, so the server can emit exact actions for each remaining foundation item. In particular, it can choose `outline` for short/mid planning or `layered_outline` for long planning without asking the client to guess.

After all foundation artifacts exist, the route asks the client to read `novel_context`, inspect the foundation fingerprint and perform a semantic cross-file review with `audit_foundation`. Writing starts only after that audit is accepted.

## Understanding `next_step.result.actions`

Each action is a machine-readable instruction. Important fields include:

- `id`: stable identifier inside this plan, such as `a1`.
- `tool`: MCP tool to call.
- `arguments`: values already determined by server facts.
- `required_inputs`: creative or semantic values the client still has to provide.
- `depends_on`: earlier action IDs that must complete first.
- `requires_revision`: whether this call needs `expected_revision`.
- `revision_source`: where to take that revision from: `plan`, a previous action ID, or `none`.
- `expected_revision_source`: the exact result field to bind, such as `next_step.revision` or `a3.revision`.
- `mode`: `required` or `choice`.
- `choice_group`: actions in the same non-empty group are mutually exclusive; choose one, do not run all of them.
- `resource_uri`: present when the same artifact can also be read through MCP Resources.

A client may use the natural-language `task` for explanation, but should prefer `actions` for actual tool scheduling.

## Revision rules

A project revision is a whole-project content fingerprint, not an incrementing counter.

For a write action:

- `arguments` already contains the known `project`;
- if `revision_source=plan`, `arguments.expected_revision` is already filled from the outer `next_step` envelope;
- if it names an action such as `a3`, bind `expected_revision` from `a3.revision` (the same source is spelled out in `expected_revision_source`);
- read-only actions use `none`.

On `REVISION_CONFLICT`, re-read current facts before deciding whether to retry. Never blindly replay an append. A duplicate `draft_chapter(mode=append)` can duplicate prose, which is why stale revisions are rejected before the write runs.

A failed write can still return a **new** revision if it changed disk state before a later checkpoint failed. Always consume the revision returned by the actual response.

## Normal chapter writing

The standard writer route is:

```text
novel_context(chapter)
→ plan_chapter
→ draft_chapter(mode=write)
→ read_chapter(source=draft)
→ check_consistency
→ semantic revision if needed
→ commit_chapter
→ next_step
```

`check_consistency` loads comparison material. It does **not** prove that the story is semantically consistent. The client AI must inspect the returned facts and the actual re-read draft.

If the draft changes after a check, re-read and check again before committing.

`commit_chapter` is the transition from draft to durable final chapter facts. Do not treat a draft as committed prose.

## Rewriting a completed chapter

A completed book or chapter should not be overwritten directly. Use `reopen_book(chapters, reason)` when a completed book needs revision.

The rewrite route is:

```text
novel_context(chapter)
→ draft_chapter(mode=write)
→ read_chapter(source=draft)
→ check_consistency
→ commit_chapter
→ next_step
```

There is deliberately no `plan_chapter` step for an already completed chapter. When the rewrite queue is empty, a previously completed book returns to the complete phase automatically.

## Long-form arcs and volumes

For layered long-form projects, the router owns structural boundaries:

- arc end → `save_review(scope=arc)`;
- then `save_arc_summary`;
- volume end → `save_volume_summary`;
- a skeleton next arc → `expand_next_arc`;
- a new volume or final volume → `save_foundation(type=append_volume, ...)`;
- a completed story → `save_foundation(type=complete_book, content={}, reason=...)` when the preconditions are satisfied.

There is **no tool named `append_volume`**. It is a `save_foundation` type.

Future outline changes use `revise_outline` and are restricted to chapters that have not already happened. Writer feedback can also result in a mutually-exclusive choice between revising the outline and resolving the feedback without a structural change.

## Recovery after a crash or lost chat

Do not try to recreate state from conversation memory.

1. Call `next_step`.
2. If a pending commit exists, it is returned first. Replay `commit_chapter` using the frozen arguments supplied by the route.
3. If you need a wider view, call `project_status`.
4. Continue with the new `next_step` plan.

The disk is the source of truth. There is no separate "resume conversation" operation.

## Read-only diagnostics

Useful read calls:

- `project_status`: current phase, progress, missing foundation, pending work, warnings.
- `verify_project`: deeper project integrity checks; it diagnoses but does not repair.
- `export_book`: committed prose only.
- `novel_context`: writing/planning context and facts.
- `read_chapter`: draft or final chapter text.
- `check_consistency`: comparison material for semantic review.

At the CLI level, `novel-mcp doctor --deep` checks configuration and all projects without printing novel text, project IDs, credentials, public hostnames, or absolute local paths.

## Common errors

| Code | Meaning | Client response |
| --- | --- | --- |
| `REVISION_CONFLICT` | Disk changed after your last read | Re-read and decide; do not blindly replay |
| `CONFLICT` | Semantic/state conflict such as stale foundation audit | Follow the message and refresh facts |
| `PRECONDITION_FAILED` | Wrong phase or missing prerequisite | Complete the required route first |
| `INVALID_REQUEST` | Bad arguments | Correct the request |
| `STORE_ERROR` | Storage read/write problem | Inspect local storage/permissions |
| `PROJECT_NOT_FOUND` | Unknown project | Select or create another project |
| `PROJECT_DAMAGED` | Missing/current-format-invalid project data | Inspect or recreate the development project |
| `PROJECT_UNSAFE` | Symlink/non-regular/over-limit project contents | Fix the project directory locally |

The project format is currently development-only and supports only the current format version; there is no compatibility migration layer yet.

## Prompts and Resources

Hosts that support MCP Prompts can use the built-in overview, architect, writer, and editor prompts. Hosts that only support Tools can call `novel_guide` for the same bundled guidance.

Hosts that support Resources can read project status, context, and chapter draft/final artifacts through `novel://...` URIs. Tool-only hosts can ignore resource URIs completely.
