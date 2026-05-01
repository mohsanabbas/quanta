# SPDD Workflow - Step-by-Step

Six steps, in order. Each step has a clear input, output, and review gate. Do not skip steps. Do not let the user skip steps - that is the whole point of choosing SPDD.

## Contents

- Step 1 - Story
- Step 2 - Analysis
- Step 3 - REASONS Canvas
- Step 4 - Code generation
- Step 5 - Verify
- Step 6 - Sync
- Tooling note (openspdd)

## Step 1 - Story

Input: A request from the user (a few sentences to a paragraph).
Output: A user story document, agreed with the user.
Review gate: User confirms the story before proceeding.

The story has exactly five sections:

1. Background - one paragraph on context: what exists today, what is changing.
2. Business Value - bulleted list of why this matters.
3. Scope In - bullets describing what this story includes.
4. Scope Out - bullets describing what this story explicitly does NOT include. (This is as important as Scope In; it bounds creep.)
5. Acceptance Criteria - three to five high-level ACs, each in Given/When/Then with concrete numeric examples.

Aim for one page total. If the request is too large for one page, split it into multiple stories following INVEST (Independent, Negotiable, Valuable, Estimable, Small, Testable) and run SPDD per story.

The story file lives alongside the Canvas with the type tag [Story], for example PROJ-001-202604291255-[Story]-add-user-roles.md. If the request is small and clearly scoped, the story content can live inline at the top of the [Analysis] file instead - but the five sections must still be present.

## Step 2 - Analysis

Input: The agreed story.
Output: An [Analysis] markdown file in the repo.
Review gate: User confirms the analysis before proceeding to the Canvas.

The analysis stays at the strategy level - no implementation detail yet. Its job is to align understanding before design.

Structure:

1. Domain concept recognition - list the entities and concepts from the story, marking Existing vs. New, with one-line descriptions and where existing ones live in the codebase.
2. Strategic approach - at a paragraph level, the proposed direction. Specifically call out the design pattern(s) being considered. State trade-offs being weighed.
3. Risks and gaps - edge cases, technical risks, ambiguities, and any AC the team has not fully covered. Be honest about uncertainty; this is the cheapest place to surface it.

Before writing the file, scan the relevant parts of the codebase to ground the analysis in real structure (not assumed structure). Use existing module names, existing class names, existing patterns.

After writing, present the analysis to the user, highlight the risks and gaps, and ask whether anything needs to change before proceeding. If the user identifies new concerns, edit the file in place rather than starting over.

## Step 3 - REASONS Canvas

Input: Reviewed [Analysis] file.
Output: A [Feat] markdown file containing the full Canvas (all seven REASONS dimensions).
Review gate: User confirms the Canvas before any code is generated.

For section-by-section guidance, read [reasons-canvas.md](./reasons-canvas.md) before drafting.

Two non-obvious things to enforce while writing the Canvas:

- Operations come last in writing, even though they are the densest section. Resist the urge to jump to "what code do we write" before Approach and Structure are settled. Operations that contradict Approach are a sign Approach was thinly considered.
- Operations must be precise enough to be unambiguous. If you can imagine two reasonable implementations of the same Operation, that Operation is underspecified. Add the missing detail (signature, ordering, acceptance) until only one reasonable implementation remains.

When the Canvas is drafted, walk the user through it section by section. Pay particular attention to Approach (this is where design judgment lives) and Safeguards (this is where the user knows hidden constraints you do not).

Mark Status: Reviewed only after the user has explicitly signed off.

## Step 4 - Code generation

Input: Reviewed [Feat] Canvas.
Output: Code that implements the Operations.
Review gate: After all Operations are implemented; before tests are written.

Rules:

- Generate task-by-task, in the Operation order specified in the Canvas. Do not jump ahead.
- Stay strictly within Operations. No "while I was there" refactors of unrelated code.
- Norms apply to every Operation. Re-read them periodically - it is easy to forget logging or naming conventions five Operations in.
- Safeguards apply to every Operation. After each Operation, ask: "is any Safeguard now at risk?" If yes, stop and address before continuing.
- If during generation an Operation turns out to be wrong or missing context, STOP. Update the Canvas first (return to Step 3, revise the affected section, walk the user through the diff). Then regenerate the affected Operation. Never silently deviate from the spec.

When generation is complete, mark the Canvas Status: Implemented.

## Step 5 - Verify

Input: Generated code.
Output: Passing API tests plus unit tests covering all ACs and Safeguards.

Two test layers:

- API / functional tests - cover the Acceptance Criteria scenarios from the story. These are end-to-end checks against running endpoints (or the equivalent for non-HTTP systems).
- Unit tests - cover Operation-level behavior, including edge cases enumerated in the Analysis's Risks and gaps section. Each Safeguard becomes at least one assertion.

If a test fails:

- If the failure is because the spec is wrong -> update the Canvas, then regenerate the relevant Operation. (Step 3 -> Step 4 -> Step 5.)
- If the failure is because generation did not follow the spec -> regenerate the Operation; do not patch the code in place.

The bias is always toward fixing the source (the Canvas) over patching the output (the code). Patching the output is what creates the spec/code drift SPDD exists to prevent.

## Step 6 - Sync

Input: Code-side changes that happen after the Canvas is finalized - refactoring, small fixes during code review, magic-number extraction, the kinds of cleanups that improve code without changing behavior.
Output: An updated Canvas reflecting those changes.

Two paths for handling post-generation changes:

| Change type | Direction | Action |
|---|---|---|
| Logic correction (changes observable behavior) | Prompt -> Code | Update the [Feat] Canvas first. Then regenerate or hand-edit code to match. Never modify behavior without updating the Canvas. |
| Refactoring (no behavior change - extract constants, rename for clarity, split a method) | Code -> Prompt | Refactor code in small, reviewed steps. Then update the affected Canvas sections (typically Operations and Norms) so the spec still describes what the code actually does. |

After syncing, mark the Canvas Status: Synced.

The discipline is: the Canvas always describes the current code. A stale Canvas is technical debt the same way stale comments are - possibly worse, because the Canvas is supposed to be the source of truth for the next change.

## Tooling note (openspdd)

Fowler's article describes a CLI tool, openspdd, that automates these steps via slash commands:

| Command | Purpose |
|---|---|
| /spdd-story | Breaks a large requirement into INVEST-sized stories. |
| /spdd-analysis | Generates the [Analysis] file from a story. |
| /spdd-reasons-canvas | Generates the full Canvas from an analysis. |
| /spdd-generate | Generates code task-by-task from the Canvas. |
| /spdd-api-test | Generates a cURL-based API test script. |
| /spdd-prompt-update | Updates the Canvas when requirements change (prompt -> code path). |
| /spdd-sync | Syncs code-side refactors back into the Canvas (code -> prompt path). |

If openspdd is installed, use these commands rather than re-implementing the workflow inline - the discipline is the same and the tool reduces drift risk. If it is not available, follow this skill's steps directly. The workflow is the discipline; the tool is a convenience.
