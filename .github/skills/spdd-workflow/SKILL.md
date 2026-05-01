---
name: spdd-workflow
description: "Applies Structured Prompt-Driven Development (SPDD) to feature work, treating prompts as first-class artifacts. Produces a REASONS Canvas (Requirements, Entities, Approach, Structure, Operations, Norms, Safeguards) and writes [Analysis] and [Feat] markdown files into the repository before any code is generated. Use ONLY when the user explicitly invokes SPDD, asks for the REASONS Canvas, mentions structured-prompt-driven development, or requests this specific methodology by name. Do not auto-apply to general coding requests, hotfixes, exploratory spikes, or one-off scripts; SPDD is intentionally heavyweight and only earns its overhead on substantive feature work the user has consciously chosen to govern this way."
---

# Structured Prompt-Driven Development (SPDD) Workflow

SPDD is a discipline for AI-assisted software development. Instead of letting a code request flow straight into generated code, the change is first captured in a structured prompt - a REASONS Canvas - that lives next to the code in version control. The Canvas defines intent, design, and boundaries; code is generated within that boundary. When reality diverges from the spec later, the rule is fix the prompt first, then update the code.

## When this skill applies

Trigger this skill ONLY when the user explicitly invokes SPDD by name - for example: "use SPDD on this," "give me a REASONS Canvas," "let's do this with structured-prompt-driven development," or by referencing the workflow steps (analysis, canvas, sync) directly.

Do NOT auto-apply this skill to general "help me write a feature" requests. SPDD has real overhead - story -> analysis -> canvas -> review -> generate -> verify -> sync. It pays back on substantive, governable feature work. It is the wrong tool for hotfixes, prototypes, or one-off scripts. Respect that. Do not volunteer it.

## The core rule

No production code is generated until a complete REASONS Canvas exists for the change, written to disk and reviewed with the user.

This is not a soft guideline. If the user invokes SPDD and then asks for code mid-conversation before the Canvas is finished, stop and finish the Canvas first. If they push back, briefly remind them they chose SPDD precisely to enforce this discipline; if they want to abandon it for this task, fine - but then this skill no longer applies and you should say so explicitly before continuing without it.

## The REASONS Canvas - seven dimensions

A complete Canvas always covers all seven, in this order:

1. R - Requirements: the problem, the business value, the Definition of Done.
2. E - Entities: domain entities, relationships, business rules. Distinguish existing vs. new.
3. A - Approach: the strategy and key design decisions for meeting the requirements (for example, "use Strategy pattern to isolate plan-specific billing formulas").
4. S - Structure: where the change lives in the system - components, modules, dependencies, layering.
5. O - Operations: concrete, ordered, testable implementation tasks. Down to method signatures, parameter types, and execution order. This is the only section the code-generation step reads when producing code.
6. N - Norms: cross-cutting engineering norms that apply to the change (naming, logging, error handling, defensive coding, project conventions).
7. S - Safeguards: non-negotiable boundaries - invariants, performance limits, security rules, things that must never happen.

For the full template with section-by-section guidance and an exemplar fragment, read [reasons-canvas.md](./references/reasons-canvas.md) before drafting a [Feat] file.

## The strict workflow

Six steps, in order. Do not skip steps. Do not collapse them.

1. Story - Frame the request as a user story (Background, Business Value, Scope In, Scope Out, Acceptance Criteria in Given/When/Then with concrete numeric examples). One page max.
2. Analysis - Identify domain concepts (existing vs. new), strategic approach, edge cases, technical risks, and AC coverage gaps. Write to an [Analysis] markdown file in the repo. Stop and review with the user.
3. REASONS Canvas - Convert the agreed analysis into a full seven-dimension Canvas. Write to a [Feat] markdown file. Operations must leave generation no creative freedom. Stop and review with the user.
4. Code generation - Generate code task-by-task, strictly following Operations order, Norms, and Safeguards. No features beyond the spec. No "while I was there" refactors.
5. Verify - Generate API tests plus unit tests covering the AC scenarios and Safeguards. Run them. If they fail, the issue is either in the spec (update the Canvas first) or in generation (regenerate from the Canvas).
6. Sync - When code is later refactored without behavior change, sync those changes back into the Canvas so the spec and code stay in lockstep.

For the detailed prescription per step (story template, analysis structure, generation rules, sync mechanics), read [workflow.md](./references/workflow.md) when starting a new SPDD cycle.

## File-naming convention

All Canvas artifacts go in the repository at a path the user specifies. Default location: docs/spdd/. Filenames follow:

```
[PROJECT-ID]-[NNN]-[YYYYMMDDHHMM]-[Type]-[kebab-slug].md
```

Where Type is one of Story, Analysis, Feat, or Test. The brackets in [Analysis], [Feat], etc. are part of the filename - keep them. Example:

```
docs/spdd/PROJ-001-202604291300-[Analysis]-add-user-roles.md
docs/spdd/PROJ-001-202604291305-[Feat]-add-user-roles.md
docs/spdd/PROJ-001-202604291320-[Test]-add-user-roles.md
```

If the user has no project ID yet, ask once and reuse it across the workflow. Use the actual current date and time (UTC). The four-digit NNN increments per artifact within a project.

## When edits arrive - two paths

Once a Canvas exists, edits are categorized by whether they change observable behavior:

- Logic correction (behavior changes): update the [Feat] Canvas FIRST, then regenerate the affected code. This is the prompt-first rule. Never patch the code and leave the Canvas stale.
- Refactoring (no behavior change - extract constants, rename for clarity, split a method): refactor code first in small steps, then sync the changes back into the affected Canvas sections (typically Operations and Norms). This is the code-first sync direction.

If you are not sure which path applies, default to prompt-first. Behavior changes silently corrupting the spec is a worse failure mode than a few extra Canvas edits.

## Reference files

Load as needed - these do not need to be read on every interaction:

- [reasons-canvas.md](./references/reasons-canvas.md) - Full REASONS Canvas template, with the structure of each section, what content belongs there, and a worked example fragment. Read this before writing a [Feat] file.
- [workflow.md](./references/workflow.md) - Detailed prescription for each of the six workflow steps: the user story template, the analysis structure, the code-generation rules, the verify/sync mechanics. Read this when starting a new SPDD cycle.

## A note on tooling

Fowler's original SPDD article describes a CLI tool called openspdd that automates these steps via slash commands (/spdd-story, /spdd-analysis, /spdd-reasons-canvas, /spdd-generate, /spdd-prompt-update, /spdd-sync). If openspdd is installed in the user's environment, prefer its commands - it enforces the same discipline more uniformly. If it is not available, follow this skill's steps directly. The workflow is the discipline; the tool is a convenience.
