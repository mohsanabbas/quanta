# REASONS Canvas - Template and Section Guidance

The Canvas is the executable blueprint for a change. It is the single artifact code generation reads. It must be complete, precise, and reviewed with the user before any code is generated.

## Contents

- Filename and frontmatter
- Document skeleton
- R - Requirements
- E - Entities
- A - Approach
- S - Structure
- O - Operations
- N - Norms
- S - Safeguards
- Worked example - billing engine excerpt

## Filename and frontmatter

[PROJECT-ID]-[NNN]-[YYYYMMDDHHMM]-[Feat]-[kebab-slug].md

The file opens with a small frontmatter block:

```markdown
# [Feat] <Short title of the change>

**ID**: <PROJECT-ID-NNN>
**Created**: <YYYY-MM-DD HH:MM UTC>
**Updated**: <YYYY-MM-DD HH:MM UTC>   <!-- present once the Canvas has been edited after creation; omit on the initial draft -->
**Status**: Draft | Reviewed | Implemented | Synced
**Related Analysis**: <relative path to [Analysis] file>

## Updates log
<!-- One bullet per substantive update. Newest first. Each bullet states the timestamp, what changed in plain language, and which sections were touched. -->
- **<YYYY-MM-DD HH:MM UTC>** - <one-sentence summary of the change>. Affected sections: <R | E | A | S | O | N | S(afeguards), comma-separated>.
```

Status advances through the workflow: Draft (still being written) -> Reviewed (user has signed off) -> Implemented (code generated and tests passing) -> Synced (any post-generation refactors reflected back).

The Updated field and the Updates log exist to make prompt-first edits auditable. When a behavior change arrives after the Canvas is reviewed, the change should be traceable to a single bullet that names what changed and where, so a reviewer can find the diff without reading the whole document. Omit both on the initial draft (the empty log is noise); add them the first time the Canvas is edited and keep them current thereafter.

## Document skeleton

```markdown
## R - Requirements
## E - Entities
## A - Approach
## S - Structure
## O - Operations
## N - Norms
## S - Safeguards
```

All seven sections are required. An empty section means "I haven't thought about this yet" - finish thinking before declaring the Canvas Reviewed.

## R - Requirements

What problem is being solved, and how do we know we are done?

Include:

- Problem statement - the user-facing or system-level need.
- Business value - the "why now."
- Scope in / scope out - what this change includes and explicitly excludes.
- Definition of Done - concrete, verifiable conditions. Use Given/When/Then with real numeric examples wherever possible.

Anti-pattern: "Make billing better." That's a wish, not a requirement.

## E - Entities

Domain entities and their relationships, plus the business rules that govern them.

For each entity:

- Mark it Existing or New.
- List its key attributes (name + type only - avoid full schemas here unless required).
- State its relationships to other entities (1:1, 1:N, N:M).
- Capture the business rules that constrain it.

If a new entity is being added, briefly state why an existing one could not carry the responsibility - this prevents quiet duplication of concepts.

## A - Approach

The chosen strategy for meeting the requirements, and the key design decisions behind it.

Include:

- Solution direction - at a paragraph level, how the requirements will be met.
- Key design decisions - specific choices (for example, "Strategy pattern over conditional branching for plan-specific billing formulas, because Plan types are an open set").
- Trade-offs accepted - what was considered and rejected, with the reason.

This section is where domain knowledge and design judgment live. Be opinionated. A vague Approach produces vague Operations.

## S - Structure

Where the change fits in the system architecturally.

Include:

- Affected components / modules / files - name them.
- New components / modules / files - name them, with a one-line purpose each.
- Layering - which architectural layer each piece belongs to (controller, service, repository, etc.) and which boundaries it must not cross.
- External dependencies - new libraries, services, or schemas this change introduces.

## O - Operations

Concrete, ordered, testable implementation steps. This is the section the code-generation step reads. It must leave no creative freedom.

Each Operation includes:

- Operation N - short imperative title.
- Target - file path and class/function/method.
- Signature - for new methods, the exact signature with parameter types and return type.
- Steps - ordered sub-steps describing what the implementation does.
- Acceptance - what makes this Operation done (typically a unit test that should pass).

Operations are ordered by dependency: an Operation that creates a new type comes before the Operation that uses it.

If you can imagine two reasonable implementations satisfying the same Operation, that Operation is underspecified. Add the missing detail (signature, ordering, acceptance) until only one reasonable implementation remains.

## N - Norms

Cross-cutting engineering conventions that apply to the change.

Examples:

- Naming conventions for new types (for example, "billing strategies named <PlanName>BillingStrategy").
- Logging requirements (level, structure, what to redact).
- Error handling style (exceptions vs. result types, error wrapping).
- Defensive coding rules (null checks, validation positioning).
- Test conventions (naming, AAA structure, fixtures location).

If the project has an existing style guide, reference it rather than restating it. Include only the norms that are non-default for this change - do not pad with universals.

## S - Safeguards

Non-negotiable boundaries. Things that must hold true after the change, regardless of how implementation proceeds.

Examples:

- Invariants - "monthly billing total never exceeds the customer's quota for Standard plan."
- Security rules - "modelId is never logged in plaintext."
- Performance limits - "p99 latency on POST /api/usage stays under 200ms."
- Backward compatibility - "existing API consumers without modelId still receive a 200 with default behavior."
- Data integrity - "no historical bill record is mutated."
- Ordering constraints / implicit upstream dependencies - "the migration that adds the modelId column runs before the migration that backfills its values," or "authentication middleware is mounted before rate-limit middleware so req.user is populated." This category captures requirements that depend on something happening earlier in a sequence - request pipeline, call chain, migration order, boot sequence, event order - where a misordering silently breaks behavior without surfacing as an obvious error. It is the easiest Safeguard category to miss because the dependency is rarely visible at the call site, only at the wiring site. When the change relies on state populated by an earlier step, name that step explicitly here.

Safeguards differ from Acceptance Criteria: ACs say what the new behavior is; Safeguards say what must remain true regardless. They become separate test assertions.

## Worked example - billing engine excerpt

A condensed fragment of a [Feat] Canvas adding model-aware pricing to a billing engine:

```markdown
## A - Approach

- Introduce BillingStrategy interface with calculate(usage, plan, modelId) returning Bill.
- Implement StandardBillingStrategy (quota + per-model overage rate) and PremiumBillingStrategy (no quota, split prompt/completion rates per model).
- BillingService resolves the strategy by plan.type via a BillingStrategyFactory.
- Trade-off considered: a switch on PlanType inside BillingService was rejected because Plan types are expected to grow (Enterprise next), and conditional branching scales poorly across many plan types.

## O - Operations

### Operation 1 - Add BillingStrategy interface
**Target**: src/billing/strategy/BillingStrategy.java
**Signature**:
```java
public interface BillingStrategy {
    Bill calculate(Usage usage, Plan plan, ModelId modelId);
}
```

**Steps**: Define interface only; no implementation in this Operation.
**Acceptance**: File compiles; referenced by Operation 2.

### Operation 2 - Implement StandardBillingStrategy

**Target**: src/billing/strategy/StandardBillingStrategy.java
**Signature**:

```java
public final class StandardBillingStrategy implements BillingStrategy {
    public Bill calculate(Usage usage, Plan plan, ModelId modelId);
}
```

**Steps**:

1. Compute remainingQuota = plan.monthlyQuota - usageThisMonth(plan.customerId).
2. Split usage.totalTokens into withinQuota and overageTokens.
3. Look up overage rate via plan.overageRates.get(modelId); if missing, throw UnknownModelException.
4. charge = overageTokens * overageRate / 1000.
5. Return Bill(customerId, withinQuota, overageTokens, charge, modelId, now()).
**Acceptance**: Unit test standard_overage_for_fast_model_yields_correct_charge passes.

```

The Approach explains why Strategy was chosen (open set of plan types). The Operations are precise enough that two engineers - or two LLMs - would generate equivalent code from them. That is the bar.
