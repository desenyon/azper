# AGENTS.md

# Azper Engineering Constitution

## 1. Purpose

Azper is a local first adaptive execution runtime.

Azper converts human intentions into durable contracts, maintains a temporal model of the user's digital world, plans and executes verified state transitions across tools and applications, learns reusable procedures from experience, and can operate through local models, APIs, subscription connections, or external agent harnesses without changing its core execution semantics.

Azper is not a chatbot with tools.

Azper is not a thin wrapper around Hermes, OpenClaw, Codex, MCP, or any model provider.

Azper is the authority that coordinates these systems.

The central execution model is:

```text
Intent
↓
Contract
↓
World State
↓
Milestone Graph
↓
Executable Frontier
↓
Capability Grant
↓
Staged Effect
↓
Execution
↓
Evidence
↓
Verification
↓
State Commit
↓
Memory Learning
↓
Replan
```

All major implementation decisions must preserve this model.

# 2. Priority Order

When requirements conflict, use this priority order.

1. Correctness

2. User intent

3. Safety and capability boundaries

4. Verified real world completion

5. Architectural integrity

6. Recoverability

7. Local usability

8. Interoperability

9. Performance

10. Developer convenience

11. Feature count

A feature that violates a higher priority principle must not be implemented merely because it is convenient.

# 3. Fundamental Architecture Rule

Reuse mechanisms.

Replace authority.

Third party systems may provide mechanisms such as:

```text
authentication

provider transports

messaging adapters

browser implementations

remote execution environments

MCP clients

ACP servers

skill parsers

tool handlers
```

Azper must retain authority over:

```text
intent interpretation

contracts

planning

world state

memory trust

capability grants

effect classification

execution scheduling

verification

success determination

learning

skill promotion

recovery

rollback
```

No compatibility layer may bypass these authorities.

# 4. Architectural Boundaries

The system is organized into these conceptual layers.

```text
Interfaces

Kernel

Intelligence Fabric

World Engine

Capability Fabric

Learning Engine

Durable Foundation

Compatibility Layer
```

Dependencies should generally move downward.

Higher level authority must never be delegated to a lower level compatibility mechanism.

# 5. Kernel Authority

The Azper Kernel owns execution truth.

The kernel contains:

```text
Contract Compiler

PlanIR

Planner

Scheduler

Executor

Effect Engine

Verifier

Recovery Engine

Capability Broker
```

No provider, model, tool, plugin, harness, messaging adapter, or compatibility subsystem may mark a Contract completed.

Only the Azper Verifier may determine that Contract success conditions have been satisfied.

# 6. Durable Execution Objects

Every meaningful run should use explicit durable objects.

Required object classes are:

```text
Contract

Run

Plan

Milestone

Step

Worker

Attempt

CapabilityGrant

Effect

Evidence

Verification

Artifact

Compensation

Commit
```

These objects should have stable identifiers.

Use sortable globally unique identifiers where practical.

Never rely on transient in memory indexes as identity.

# 7. Contract Model

A Contract represents what the user actually requested.

A Contract should support:

```text
objective

deliverables

success conditions

invariants

constraints

budgets

privacy requirements

approval requirements

verification requirements

expiration

scope
```

Plans may change.

Models may change.

Workers may fail.

Tools may disappear.

The Contract remains the semantic source of truth for the Run.

A Contract must never be silently relaxed because execution becomes difficult.

If a Contract cannot be completed, report the unmet conditions explicitly.

# 8. Contract Compilation

Natural language requests should be converted into typed Contracts when durable execution is useful.

Do not overformalize trivial conversation.

Use Contract compilation when the request involves any meaningful combination of:

```text
multiple actions

persistent state

external mutations

background work

long running execution

parallel work

recovery

automation

verification

future triggers
```

The Contract Compiler may use a model for semantic interpretation.

Deterministic code validates the resulting structure.

The model never validates its own Contract.

# 9. PlanIR

Plans are represented through an explicit intermediate representation named PlanIR.

Each executable node should be capable of containing:

```text
objective

dependencies

preconditions

postconditions

required capabilities

candidate tools

expected effects

verification strategy

compensation strategy

resource locks

estimated cost

estimated latency

risk class

parallelization eligibility

failure policy
```

PlanIR must be machine validated before execution.

Models may propose PlanIR.

The planner runtime determines whether the proposed graph is structurally acceptable.

# 10. Planning Strategy

Azper uses hierarchical receding horizon planning.

Do not generate a large rigid plan once and execute it blindly.

Planning has three levels.

## Contract

The stable final objective.

## Milestones

Relatively stable intermediate objectives.

## Execution Frontier

The small set of immediately executable Steps.

The runtime loop is:

```text
Observe
↓
Update World State
↓
Select Milestone
↓
Generate Frontier
↓
Validate Frontier
↓
Execute
↓
Verify
↓
Commit State
↓
Replan
```

Plans should adapt to observed reality.

Previously verified Milestones should remain stable unless later Evidence invalidates them.

# 11. Planning Validation

Before a Step enters execution, validate at least:

```text
Contract relevance

dependency satisfaction

precondition validity

tool availability

capability availability

risk classification

observability

verification feasibility

resource conflicts
```

A Step that cannot be meaningfully verified should be marked with weaker assurance.

Never pretend weak observability is strong verification.

# 12. Workers

Azper uses ephemeral Workers rather than unrestricted hierarchies of autonomous agents.

A Worker receives:

```text
objective

immutable Context Pack

capabilities

budget

output schema

evidence requirements
```

Workers must not be allowed to:

```text
grant themselves permissions

change their Contract

silently broaden scope

write directly into curated memory

mark their parent Run complete

modify unrelated world state

spawn unlimited descendants

ignore cancellation

bypass Effect classification
```

Workers return results and Evidence to the Kernel.

The Kernel decides what to trust.

# 13. Context Packs

Every significant model invocation should be reproducible from a Context Pack.

A Context Pack should contain references to:

```text
Contract

Plan node

world snapshot

relevant memory

available tools

capabilities

privacy policy

model role

token budget
```

Context Packs should be immutable once execution begins.

Store enough provenance to reconstruct why a Worker had access to particular information without storing hidden model reasoning.

# 14. Effects

All consequential external state changes are Effects.

Examples include:

```text
file writes

Git operations

remote record updates

messages

calendar changes

database mutations

deployments

account configuration

browser submissions

process termination
```

Every Effect should be classified before execution.

Recommended classes include:

```text
Pure

Read

ReversibleWrite

CompensatableWrite

IrreversibleWrite

ExternalCommunication

FinancialEffect

PrivilegedSystemAction
```

Effect classification drives approval policy, execution policy, verification, and recovery.

# 15. Staged Execution

Where practical, mutations should follow:

```text
Observe

Prepare

Stage

Validate

Approve when required

Execute

Read back

Verify

Commit
```

Avoid direct mutation from model output.

The model proposes intent.

The Effect Engine performs governed execution.

# 16. Idempotency and Reconciliation

Never blindly retry a potentially mutating operation after an ambiguous failure.

Classify failures.

Recommended categories include:

```text
Transient

Authentication

Permission

RateLimit

MalformedRequest

ToolUnavailable

StateConflict

PreconditionInvalidated

AmbiguousOutcome

PermanentFailure
```

An AmbiguousOutcome requires reconciliation before retry.

Example:

```text
send request
↓
connection lost
↓
unknown whether remote server committed action
↓
inspect remote state
↓
decide whether retry is safe
```

Avoid duplicate messages, duplicate payments, duplicate issues, duplicate events, and duplicate mutations.

# 17. Verification

Verification is separate from execution.

A tool returning success is Evidence.

It is not necessarily proof.

Verification should evaluate Contract or Step postconditions against observed state.

Examples:

```text
file write
→ read file and hash contents

calendar update
→ fetch event again

Git commit
→ inspect repository state

deployment
→ query deployed endpoint

database mutation
→ query affected row

browser submission
→ inspect resulting page or remote state
```

Prefer independent verification when practical.

For high consequence work, verification may use a different model or deterministic validator than the executor.

# 18. Evidence

Evidence must be structured and attributable.

Evidence may include:

```text
tool output

file hash

diff

test result

HTTP result

database record

remote object identifier

screenshot

DOM state

Git commit

artifact

structured observation
```

Every Evidence record should store its source.

Never allow a model generated statement to count as Evidence by itself.

# 19. Completion

A Run is complete only when required Contract success conditions have Verification records that satisfy the Contract's assurance requirements.

Possible terminal states should distinguish at least:

```text
Completed

PartiallyCompleted

Blocked

Failed

Cancelled

Superseded
```

Do not collapse these into generic success or failure.

# 20. Compensation and Undo

Effects should declare compensation behavior when possible.

Use three broad categories:

```text
Reversible

Compensatable

Irreversible
```

Azper should eventually support commands such as:

```text
azper undo RUN_ID
```

Undo must operate from recorded Effects and Compensation strategies.

Do not implement undo through speculative inverse prompts.

# 21. World Engine

Azper maintains a temporal model of the user's digital environment.

The World Engine should model:

```text
Entities

Relations

Facts

Events

Commitments

Projects

People

Documents

Repositories

Accounts

Devices

Tasks

Deadlines

Decisions
```

The World Engine is not a vector store.

It is structured state with provenance and time.

# 22. Temporal Facts

Facts should support:

```text
subject

predicate

object

observed_at

valid_from

valid_to

confidence

origin

source

scope

supersession
```

When reality changes, do not destroy historical truth.

Close the previous validity interval and create a new fact.

Azper should be able to answer both:

```text
What is true now?
```

and:

```text
What did we believe was true at time T?
```

# 23. Memory Architecture

Memory is divided into distinct classes.

## Working Memory

Temporary state needed during current reasoning or execution.

## Episodic Memory

Records of what happened.

## Semantic Memory

Facts believed to be true.

## Temporal World Memory

Versioned state and relations over time.

## Preference Memory

Current user directives and preferences.

## Procedural Memory

Knowledge about how to perform recurring work.

## Prospective Memory

Future intentions compiled into runtime triggers.

## Execution Memory

Experience learned from prior Runs, including failures, recoveries, tools, plans, verification outcomes, cost, and environment state.

Do not collapse these memory classes into one generic embedding store.

# 24. Memory Authority

SQLite is the authoritative local memory store unless a later Architecture Decision explicitly changes this.

Vector indexes are secondary and rebuildable.

Full text indexes are secondary and rebuildable.

Derived graph projections are secondary and rebuildable if their canonical facts remain available.

Do not make an external vector database mandatory for core operation.

# 25. Memory Write Pipeline

Durable memory writes follow:

```text
Observation
↓
Provenance classification
↓
Candidate extraction
↓
Entity resolution
↓
Temporal normalization
↓
Conflict detection
↓
Trust gating
↓
Write
↓
Index
```

No arbitrary model text should directly become trusted memory.

# 26. Memory Provenance

Memory must distinguish origins such as:

```text
Owner

TrustedConnector

AgentDerived

External

System

RecalledMemory
```

Unknown external information defaults toward lower trust.

Do not promote external web content into user preferences or trusted personal facts without evidence.

# 27. Recall Loop Prevention

Recalled memory must be marked as recalled content.

Content that entered a model invocation because it was recalled must not automatically become a new memory candidate merely because the model repeated it.

One fact recalled one hundred times should remain one fact.

# 28. Preference Memory

Preferences are active directives, not historical observations.

Store:

```text
directive

scope

status

confidence

observed_at

supersedes
```

When a preference changes, supersede the previous version.

Do not leave contradictory current preferences active simultaneously.

# 29. Prospective Memory

Future actions should become runtime objects rather than prose memories.

Example:

```text
User:
Tell me when pull request 41 merges.
```

Compile into:

```text
Intent

Condition
PR 41 state equals merged

Action
Notify user

Expiration
optional
```

Do not rely on the language model remembering to check later.

# 30. Execution Memory

Execution Memory is one of Azper's primary differentiators.

For significant Runs, preserve:

```text
Contract

Plan

environment

tools used

models used

failures

recoveries

successful procedures

verification

user corrections

latency

cost
```

Future planning should be able to retrieve similar successful and failed executions.

Do not merely retrieve conversation snippets.

Retrieve experience.

# 31. Memory Maintenance

Periodic memory maintenance may perform:

```text
deduplication

entity merging

fact supersession

procedure extraction

experience extraction

preference reconciliation

stale fact detection

index rebuilding

importance adjustment
```

Maintenance operates on derived memory representations.

Source Evidence should remain recoverable.

# 32. Memory Versioning

Curated memory changes should be versioned.

Azper should support eventual operations such as:

```text
memory history

memory diff

memory rollback
```

A mistaken memory update should be recoverable.

# 33. Context Compiler

Do not construct prompts by dumping all available context.

Every model call passes through the Context Compiler.

The Context Compiler considers:

```text
model role

model capabilities

Contract

Plan node

world state

retrieved memory

tools

capabilities

privacy

context limit

cost constraints
```

It produces the smallest sufficient context.

Context quality is more important than context quantity.

# 34. Model Fabric

Separate model identity from execution runtime.

Use concepts equivalent to:

```text
ModelRef

RuntimeRef

AuthProfile

ExecutionRoute
```

Example ModelRefs:

```text
openai MODEL

anthropic MODEL

google MODEL

local MODEL
```

Example RuntimeRefs:

```text
api

openai_compatible

oauth

codex

acp

claude_cli

gemini_cli

llama_cpp

ollama

vllm

mlx
```

A model is not the same thing as the transport used to execute it.

# 35. Model Roles

Azper should support different models for different roles.

Examples:

```text
Planner

Executor

Verifier

MemoryExtractor

MemoryReranker

Vision

Embedding

Router

CodeWorker

Summarizer
```

Do not assume the strongest general model should perform every task.

# 36. Routing

The Model Router should consider:

```text
task category

historical performance

required modalities

tool support

structured output reliability

privacy

context requirements

cost

latency

availability
```

Routing should eventually learn from Azper's own verified performance data.

Prefer measured reliability over marketing claims.

# 37. Local First Requirement

Azper must remain meaningfully functional without cloud services.

Local execution must support:

```text
local model

filesystem

shell

memory

planning

verification

skills

automation

TUI
```

Cloud providers extend Azper.

They do not define Azper.

# 38. Local Model Backends

The runtime architecture should support multiple local inference backends.

Priority examples include:

```text
llama.cpp

Ollama

vLLM

MLX
```

Do not tightly couple the Kernel to any one local inference server.

# 39. Privacy Routing

Projects, memories, Contracts, and individual Steps may carry privacy policy.

Example:

```text
local_only
```

When `local_only` applies, remote ExecutionRoutes are ineligible.

This must be enforced by deterministic routing policy.

Do not rely on prompt instructions asking the model to preserve privacy.

# 40. Harness Interoperability

Azper should support external agent harnesses.

ACP is a major interoperability protocol.

Azper should eventually support both:

```text
Azper as ACP client

Azper as ACP agent
```

Dedicated adapters may exist for harnesses when richer semantics are available.

Harnesses act as Workers or execution mechanisms.

They do not become the Azper Kernel.

# 41. Hermes Reuse Policy

Hermes is MIT licensed.

Azper may reuse, modify, and distribute Hermes code while preserving required copyright and license notices.

Hermes should be treated as a source of proven mechanisms.

Preferred reuse areas include:

```text
provider transports

provider authentication

OAuth handling

custom OpenAI compatible endpoints

remote terminal environments

messaging platform adapters

MCP protocol plumbing

ACP protocol plumbing

browser implementations

selected tool implementations

skill parsing

skill discovery

skill learning utilities

plugin compatibility
```

Azper features and semantics always take precedence.

# 42. Hermes Architectural Boundary

All Hermes reuse lives behind an explicit compatibility boundary.

Preferred location:

```text
internal/compat/hermes/
```

Nothing in the Kernel should import Hermes internal domain models directly.

Translate Hermes objects into Azper objects.

Example:

```text
Hermes tool
↓
Hermes adapter
↓
Azper ToolManifest
↓
Capability classification
↓
Effect classification
↓
Azper execution
```

The planner should not know whether a tool is implemented by Hermes, native Go, MCP, Wasm, or another backend.

# 43. Do Not Inherit Hermes Authority

Do not use the Hermes AIAgent as Azper's primary execution loop.

Do not use Hermes memory as Azper's authoritative memory system.

Do not use Hermes planning as Azper's primary planner.

Do not use Hermes command approval regexes as Azper's security boundary.

Do not use Hermes success semantics as Azper completion semantics.

Do not use Hermes session state as Azper world state.

Hermes may provide compatibility behavior underneath Azper.

# 44. Hermes Compatibility Process

Hermes integration should be pinned to an explicit upstream commit.

Track:

```text
upstream repository

upstream commit

local modifications

synchronization date

license
```

Do not automatically track upstream main.

Updates require compatibility tests and AzperBench evaluation.

# 45. Compatibility Worker

Because Hermes is primarily Python and Azper Core is Go, isolate Hermes through a compatibility Worker when required.

Preferred architecture:

```text
Azper Go daemon
↓
framed structured RPC over stdio
↓
Hermes compatibility process
```

Do not expose the compatibility process unnecessarily over network ports.

The user should still interact with one Azper installation.

# 46. Skills

Azper should remain compatible with the broader Agent Skills ecosystem where practical.

Hermes compatible skills should work with minimal or zero conversion.

Azper may extend skill metadata with fields such as:

```text
capabilities

verification

risk

evaluation

source Runs

promotion status
```

Do not unnecessarily fork open skill standards.

# 47. Skill Learning

Repeated successful behavior may become a Skill candidate.

Lifecycle:

```text
Execution traces
↓
Pattern detection
↓
Procedure candidate
↓
Generated Skill
↓
Static validation
↓
Security validation
↓
Historical replay
↓
Sandbox evaluation
↓
Shadow execution
↓
Canary use
↓
Production promotion
```

Generating a Skill does not mean the Skill is trusted.

# 48. Procedural Compilation

Repeated deterministic reasoning should eventually compile into normal procedures.

Do not force an LLM to rediscover predictable workflows forever.

Convert stable sequences into:

```text
code

PlanIR templates

validated scripts

tool pipelines
```

Keep model reasoning for ambiguous decisions.

# 49. Learning and Self Improvement

Azper may improve:

```text
planning prompts

routing policies

retrieval policies

context budgeting

verification strategy

tool selection

skills

procedures
```

Changes use champion and challenger evaluation.

```text
Current behavior
↓
Candidate behavior
↓
Historical replay
↓
AzperBench
↓
Safety suite
↓
Cost evaluation
↓
Latency evaluation
↓
Shadow evaluation
↓
Promotion decision
```

No self generated improvement becomes production behavior merely because a model says it is better.

# 50. Core Self Modification

Azper must not autonomously rewrite its own Kernel and silently deploy the result.

Core code changes follow the normal repository development and review process.

Learning systems may propose patches.

They may not bypass tests, review, or explicit promotion.

# 51. Tools

Every tool should expose an Azper ToolManifest.

The manifest should support concepts such as:

```text
name

description

input schema

output schema

effects

capabilities

idempotency

verification

compensation

availability

transport
```

Tool implementations may come from:

```text
Azper native code

Hermes compatibility

MCP

OpenAPI

Wasm

local executables

external harnesses
```

The Kernel consumes the same abstraction.

# 52. Capability Security

Authorization is capability based.

Avoid broad permissions such as:

```text
shell access
```

Prefer scoped grants such as:

```text
filesystem.read
scope project directory

filesystem.write
scope project source directory

github.pull_request.write
scope repository

network.connect
scope specific domain
```

Capabilities should support:

```text
scope

expiration

Run binding

Worker binding

Effect class

approval source
```

Workers inherit only necessary capabilities.

# 53. Taint and Content Trust

External data and instructions are not equivalent.

Track content provenance.

Recommended categories include:

```text
trusted owner instruction

trusted local state

authenticated connector data

external content

unknown content

recalled memory
```

The Context Compiler should maintain a clear boundary between instructions and untrusted data.

Prompt injection in a web page must not become Azper policy.

# 54. Secrets

Do not store secrets directly in normal Azper configuration when an operating system credential store can be used.

Configuration should reference credentials.

Secrets should remain scoped to the node and mechanism that require them where practical.

Never log secrets.

Never include secrets in Evidence.

Never expose secrets in Trace views.

# 55. Automation

Automations use the same Kernel as interactive work.

Triggers may include:

```text
time

cron

filesystem

webhook

email

calendar

GitHub

world state

connector event
```

A trigger creates or resumes a normal Contract.

Do not create a second weaker automation engine.

# 56. Event Log

Durable state changes should emit structured events.

Examples:

```text
ContractCreated

RunStarted

PlanCreated

WorkerStarted

CapabilityGranted

EffectStaged

EffectExecuted

EvidenceRecorded

VerificationPassed

MemoryCandidateCreated

RunCompleted
```

SQLite is sufficient for local event persistence.

Do not introduce distributed infrastructure without demonstrated need.

# 57. Crash Recovery

Running work must be recoverable after process termination when possible.

Workers should use leases.

On restart:

```text
inspect active Attempts

inspect lease state

inspect external Effects

reconcile ambiguous Effects

resume safe work

retire stale work
```

Do not simply mark every interrupted operation as failed.

# 58. Artifacts

Important outputs become Artifacts.

Examples:

```text
files

diffs

reports

screenshots

test results

commits

documents

message drafts

browser captures
```

Artifacts should support provenance and content addressing.

Use a modern cryptographic content hash such as BLAKE3 unless a specific interoperability requirement dictates otherwise.

# 59. Repository Technology

Preferred core language:

```text
Go
```

Primary reasons:

```text
single binary distribution

concurrency

process management

networking

cross platform support

SQLite support

direct Charm ecosystem integration
```

Python is acceptable for compatibility Workers, model experimentation, evaluation, and third party ecosystems.

TypeScript is acceptable for SDKs and future graphical interfaces.

Do not introduce another language without a concrete technical reason.

# 60. TUI Architecture

The terminal interface is a first class product.

Preferred stack:

```text
Bubble Tea v2

Ultraviolet

Lip Gloss v2

Bubbles v2

Huh v2

Glamour v2

Harmonica where useful
```

Bubble Tea owns terminal state.

Do not let multiple frameworks independently control stdin, stdout, terminal raw mode, alternate screen state, or rendering.

# 61. TUI Product Standard

The TUI must not resemble a debug console.

It should communicate:

```text
what Azper is doing

why the action matters

what changed

what remains

what requires approval

whether results were verified
```

Avoid raw tool JSON unless the user explicitly opens developer detail.

Avoid displaying hidden reasoning.

Display semantic execution state.

# 62. TUI Design Language

Prefer:

```text
whitespace

strong hierarchy

subtle separators

limited accent colors

minimal borders

quiet animation

high information density only when requested
```

Avoid:

```text
giant ASCII branding

constant decorative animation

neon overload

nested boxes everywhere

fake initialization steps

raw infrastructure logs

endless thinking indicators
```

Azper should feel like a serious instrument.

# 63. TUI Responsive Modes

Support at least:

```text
Wide

Medium

Narrow
```

Wide may show navigation, workspace, and inspector.

Medium may show navigation and workspace.

Narrow should become a focused single pane application.

Terminal width must not destroy usability.

# 64. TUI Core Screens

Plan for these primary screens:

```text
Ask

Work

Runs

World

Memory

Skills

Automations

Models

Tools

Trace

Settings
```

Do not expose every implementation object at top level.

Use progressive disclosure.

# 65. TUI Activity

Display semantic states such as:

```text
Planning

Inspecting

Working

Testing

Verifying

Recovering

WaitingForApproval

Completed
```

Each state may expand into structured details.

Do not expose private model reasoning.

# 66. Tool Cards

Render model tool interactions as product level cards.

Example:

```text
Shell

go test ./...

34 passed

6.4 seconds
```

Raw arguments and results belong in a details view.

# 67. Approval UX

Approval prompts must communicate:

```text
requested action

scope

expected Effects

risk

reversibility

verification strategy
```

Support grouped approval when multiple related Effects have identical trust characteristics.

Reduce permission fatigue without weakening capability boundaries.

# 68. Composer

The composer should eventually support:

```text
multiline input

large paste

history

draft restoration

completion

command palette

mentions

context attachments

file attachment

image attachment where supported
```

Useful mention classes may include:

```text
project

file

person

Run

memory

artifact
```

# 69. Command Palette

A discoverable command palette should coexist with expert CLI commands.

Do not force users to memorize slash commands.

# 70. Model Picker

Model selection should expose useful information such as:

```text
provider

runtime

authentication

context

tool capability

vision capability

privacy

local or remote

historical Azper performance

estimated cost
```

Model selection should feel like choosing execution infrastructure, not selecting a string identifier.

# 71. World Interface

Users must be able to inspect what Azper believes.

World views should expose:

```text
entities

relations

deadlines

projects

people

commitments

sources

confidence

history
```

Azper should not maintain an invisible personal model that users cannot inspect.

# 72. Memory Interface

Users should be able to inspect:

```text
facts

preferences

episodes

procedures

intentions

experience
```

Provide paths toward:

```text
edit

forget

view source

view history

rollback
```

# 73. Accessibility

Support:

```text
keyboard only operation

reduced motion

high contrast

monochrome

ASCII safe mode

screen reader mode

non alternate screen mode
```

Accessibility must not be treated as cosmetic polish.

# 74. API

The daemon should expose a stable local programmatic interface.

Conceptual resources include:

```text
Contracts

Runs

World

Memory

Models

Tools

Automations
```

Support streaming execution events.

Provide SDKs only after the daemon API becomes sufficiently stable.

# 75. Testing Philosophy

Tests are part of architecture.

Every major subsystem should have:

```text
unit tests

integration tests

failure tests

restart tests where relevant

property tests where useful

security tests

regression tests
```

External Effect code needs failure injection.

Do not test only the happy path.

# 76. Verification Tests

For each Effect class, create tests proving that:

```text
successful execution produces Evidence

failed execution does not produce false completion

ambiguous outcomes trigger reconciliation

retries do not duplicate Effects

compensation restores expected state where supported

stale Workers cannot commit
```

# 77. Memory Tests

Test:

```text
temporal updates

supersession

contradictions

source provenance

recall loops

poisoning resistance

preference replacement

entity resolution

project scoping

memory rollback

execution memory retrieval
```

Use established long horizon memory benchmarks where practical.

# 78. Planner Tests

Test:

```text
dependency handling

unsatisfiable Contracts

missing tools

tool failure

state changes during execution

parallelizable work

resource conflicts

replanning

partial completion

budget exhaustion
```

Do not evaluate planning only through qualitative examples.

# 79. AzperBench

Maintain a benchmark suite covering real agent work.

Measure:

```text
TaskCompletion

VerifiedCompletion

FalseSuccess

RecoveryRate

MemoryRecall

TemporalRecall

PreferenceAdherence

ProceduralRecall

ProspectiveMemory

HumanInterventions

ApprovalCount

RollbackSuccess

Latency

TokenUsage

Cost
```

The primary product metric is:

```text
Verified Autonomous Completion Rate
```

This means the percentage of tasks where Azper independently reaches the requested external state and can produce sufficient Evidence that the result is correct.

# 80. Performance

Optimize only after correctness instrumentation exists.

Performance priorities:

```text
fast startup

responsive TUI

low idle resource usage

efficient SQLite access

minimal context construction

bounded concurrency

low unnecessary model usage
```

Do not trade away durable truth for apparent speed.

# 81. Logging

Structured logs should go to files or dedicated debug output.

Do not corrupt TUI rendering with direct stdout logging.

Logs must not contain secrets.

Run Trace is user facing structured observability.

Debug logs are developer facing diagnostics.

Keep these concepts separate.

# 82. Configuration

Configuration should be:

```text
human readable

versioned

migratable

validated

cross platform
```

Project configuration and user configuration should have explicit precedence.

Do not silently execute arbitrary project configuration as code unless the user has explicitly trusted that repository.

# 83. Dependencies

Prefer mature, narrowly scoped dependencies.

Before adding a dependency, evaluate:

```text
maintenance

license

security

portability

binary impact

replaceability

whether standard library functionality is sufficient
```

Avoid introducing a framework merely because it saves a small amount of code.

# 84. Licensing

Maintain:

```text
THIRD_PARTY_NOTICES

license copies

source attribution

upstream commit records
```

When reusing Hermes or another project, preserve all legally required notices.

Do not remove attribution.

Do not copy code whose license is incompatible with Azper's intended distribution model.

# 85. Code Quality

Prefer:

```text
small explicit interfaces

clear ownership

typed state

deterministic validation

structured errors

dependency injection where useful

context aware cancellation

immutable inputs where practical
```

Avoid:

```text
global mutable state

magic string routing

silent fallbacks

untyped maps for core domain objects

hidden side Effects

fire and forget mutation

massive central files

model prompts containing business logic that belongs in code
```

# 86. Error Handling

Errors should preserve:

```text
category

source

operation

retryability

ambiguity

user relevance

underlying cause
```

Do not flatten all errors into strings.

Do not swallow errors merely to keep the agent moving.

# 87. Cancellation

Cancellation must propagate through:

```text
Run

Planner

Worker

model call

tool call

process

browser operation

remote compatibility Worker
```

Late results from cancelled Attempts must not be allowed to overwrite current state.

# 88. Concurrency

Concurrency must be bounded.

Parallel work is allowed only when:

```text
dependencies permit it

resource locks permit it

capabilities permit it

Effects cannot conflict
```

Results must join deterministically.

Do not allow completion order to silently alter semantic state.

# 89. Repository Editing Protocol

When modifying an existing codebase:

1. Inspect repository state before editing.

2. Identify relevant architecture and tests.

3. Preserve unrelated user changes.

4. Make focused changes.

5. Read modified files back.

6. Run targeted validation.

7. Run broader validation when justified.

8. Inspect resulting diff.

9. Record remaining uncertainty.

Never overwrite unrelated dirty work.

Never assume a file remained unchanged between reading and writing when concurrent edits are possible.

# 90. Implementation Protocol for Codex

For every nontrivial task:

1. Read this AGENTS.md.

2. Inspect the relevant repository area.

3. State the concrete implementation objective internally.

4. Identify architectural invariants affected.

5. Search for existing reusable code before creating parallel implementations.

6. Prefer extending established interfaces over bypassing them.

7. Implement the smallest coherent architectural slice.

8. Add or update tests.

9. Run validation.

10. Inspect the final diff.

11. Update living documentation when the architecture materially changed.

12. Update the Evolution Log when the lesson should influence future engineering.

Do not merely satisfy the visible test if the implementation violates the system design.

# 91. Search Before Rebuild

Before creating infrastructure for:

```text
providers

authentication

MCP

ACP

messaging

remote execution

skills

browser tooling

tool handlers
```

inspect the Hermes compatibility source and existing Azper abstractions.

Reuse when the mechanism is already good enough.

Do not introduce duplicate implementations without a documented reason.

# 92. Native Rewrite Policy

A reused Hermes subsystem should be rewritten natively only when at least one concrete reason exists.

Examples:

```text
performance

security

distribution size

dependency complexity

poor maintainability

incompatible semantics

missing functionality

reliability

platform limitations
```

Do not rewrite for aesthetic purity.

# 93. No Silent Degradation

If a preferred feature is unavailable, Azper may fall back only when the fallback preserves the Contract's important semantics.

Example:

```text
preferred provider unavailable
↓
fallback provider supports required tools and privacy
↓
continue
```

Bad example:

```text
verification unavailable
↓
pretend tool success equals Contract success
```

If semantics cannot be preserved, surface the limitation.

# 94. No Fake Features

Do not add UI elements, configuration options, status messages, or documentation claiming functionality that is not wired end to end.

Every visible feature must correspond to real runtime behavior.

# 95. No Demo Specific Architecture

Do not hardcode behavior solely to make benchmark or demo scenarios look successful.

Benchmarks should shape general mechanisms.

They should not create special cases for benchmark inputs.

# 96. Current Build Order

Unless an explicit task changes priority, prefer this sequence.

## Foundation

```text
Go project

SQLite store

event log

Contract types

Run types

Effect types

Evidence types

Verification types

basic TUI shell
```

## Kernel

```text
Contract Compiler

PlanIR

receding horizon planner

scheduler

Workers

Effect Engine

Verifier

recovery
```

## Intelligence Fabric

```text
provider abstraction

Hermes provider bridge

local inference

model routing

Context Compiler

ACP

Codex harness
```

## Memory

```text
episodic memory

semantic facts

temporal World Engine

preferences

execution memory

retrieval

versioning
```

## External Work

```text
MCP

browser

GitHub

calendar

email

automations

capabilities

compensation
```

## Learning

```text
trace mining

procedural memory

Skill generation

evaluation

shadow execution

promotion

adaptive routing
```

## Expansion

```text
messaging channels

remote nodes

desktop interface

mobile companion

additional harnesses
```

# 97. Product Anti Goals

Azper should not become:

```text
a giant prompt

a tool count competition

a thin model wrapper

a collection of disconnected agents

a vector database with chat

a coding agent only

a cloud dependent assistant

a hidden autonomous daemon with no observability

a clone of Hermes

a clone of OpenClaw

a clone of Aiden
```

# 98. Definition of Done

A feature is not done because it compiles.

For significant functionality, Done means:

```text
architecture is coherent

implementation exists

tests exist

failure behavior exists

cancellation is considered

observability exists

security implications are considered

documentation is updated

no known false success path remains

user visible behavior is truthful
```

For state mutating features, also require:

```text
Effect classification

Evidence

Verification

recovery

reconciliation where applicable
```

# 99. Architecture Decisions

Material architecture decisions should be recorded under:

```text
docs/adr/
```

Use ADRs for decisions such as:

```text
changing authoritative storage

changing core protocol boundaries

introducing new runtime languages

replacing Hermes subsystems

altering Contract semantics

altering capability semantics

changing event persistence

changing memory authority
```

An ADR should record:

```text
Context

Decision

Alternatives

Consequences

Migration
```

# 100. Evolution Model

This file is intentionally living documentation.

It contains two classes of rules.

## Constitutional Rules

Sections defining Azper's identity and authority model are constitutional.

Examples include:

```text
Azper owns execution truth

Contracts define requested outcomes

Effects are explicit

Verification is separate from execution

memory has provenance

models are not authoritative state

Hermes provides mechanisms rather than authority

local execution remains first class
```

Codex must not weaken constitutional rules autonomously.

Changes require an explicit Architecture Decision and user approved architectural direction.

## Operational Rules

Implementation guidance may evolve.

Examples include:

```text
specific libraries

directory layouts

backend choices

performance techniques

testing utilities

provider implementations

migration strategies
```

Codex may update these when repository evidence demonstrates a better approach.

# 101. Updating AGENTS.md

Update this file when a discovered engineering lesson satisfies all of the following.

1. The lesson is likely to matter beyond the current task.

2. The lesson changes how future work should be performed.

3. The lesson is supported by repository evidence, tests, benchmark results, or explicit user direction.

4. The lesson does not contradict constitutional rules.

Prefer precise additions over broad generic advice.

Do not grow this file with trivial observations.

# 102. Evolution Sections

Maintain these sections near the end of the file.

## Current Architecture Notes

Concise facts about the current implementation that future agents need.

## Known Architectural Debt

Important compromises that should not accidentally become permanent assumptions.

## Active Experiments

Experimental systems currently under evaluation.

## Evolution Log

Durable lessons and major changes.

# 103. Current Architecture Notes

Update as implementation progresses.

Initial assumptions:

```text
Core language
Go

Primary local store
SQLite

Primary TUI
Bubble Tea v2

Rendering
Lip Gloss v2 and Ultraviolet

Forms
Huh v2

Markdown
Glamour v2

Compatibility source
Hermes through isolated Python compatibility process

Compatibility transport
structured stdio RPC

Primary execution architecture
Contract plus PlanIR plus Effects plus Verification

Memory authority
Azper World and Memory Engine

Provider authority
Azper Model Router

Provider mechanisms
Azper native plus Hermes compatibility

Local model goal
llama.cpp plus optional Ollama, vLLM, and MLX adapters
```

These are implementation defaults, not immutable constitutional rules.

Implemented foundation as of 2026 08 08:

```text
Go module with SQLite schema migrations through version 4

Durable Contract, Run, PlanIR, CapabilityGrant, Effect, Artifact, Evidence, Verification, and Compensation records

Atomic aggregate and event persistence

Deterministic PlanIR dependency, observability, capability, Effect, risk, and compensation validation

One scoped local file ReversibleWrite Effect with BLAKE3 content addressing

Durable pre-mutation Executing state and idempotent ambiguous-outcome reconciliation

Independent filesystem read-back Verification before Effect Commit

Durable file Compensation with independently verified restore or removal

Bounded manual recovery scan for durable Executing file Effects and Compensations

Contract-level success evaluation, Run completion, automatic startup recovery, and TUI remain unimplemented
```

# 104. Known Architectural Debt

Initially expected debt includes:

```text
Hermes compatibility process introduces Python distribution complexity.

Some reused Hermes tools will not initially expose perfect Effect metadata.

Some MCP tools will have incomplete reversibility information.

Temporal entity resolution will initially be heuristic.

Model routing will initially use manually designed policies before enough Azper performance data exists.

Skill promotion will initially require more human supervision before automated evaluation becomes mature.

Some external systems will provide weak verification surfaces.
```

Do not hide these limitations.

Design migration paths.

# 105. Active Experiments

This section may change frequently.

Initial areas worth experimentation include:

```text
temporal graph retrieval

execution memory retrieval

adaptive Context Compiler budgets

planner frontier size

independent verifier model selection

procedure compilation

model routing from verified historical success

automatic Effect classification

semantic rollback

learned tool selection

shadow Skill promotion
```

Experimental behavior must remain isolated behind explicit flags or interfaces when it could threaten correctness.

# 106. Evolution Log

Use entries in this format:

```text
YYYY MM DD

Change:
What changed.

Evidence:
Why it changed.

Impact:
What future agents should do differently.

Status:
Active, Experimental, Superseded, or Reverted.
```

Initial entry:

```text
2026 08 08

Change:
Established Azper as an independent Go execution runtime with Hermes reused behind a compatibility boundary.

Evidence:
Hermes provides mature MIT licensed implementations for providers, authentication, remote execution, messaging, MCP, ACP, Skills, and other integration mechanisms. Rebuilding these mechanisms would delay development without strengthening Azper's core differentiation.

Impact:
Reuse Hermes mechanisms aggressively when appropriate, but never delegate Contract, planning, World, memory authority, capabilities, Effects, Verification, recovery, or learning authority to Hermes.

Status:
Active
```

```text
2026 08 08

Change:
Established durable pre-mutation Effect state, idempotency, reconciliation, executor Evidence, and independent Verification as the required pattern for external mutations.

Evidence:
The local file Effect test suite forces Evidence persistence to fail after the file replacement succeeds, then proves that retry observes the desired BLAKE3 artifact and completes without a duplicate write. Separate tests prove target drift fails Verification, scope and symlink escapes are rejected, expired grants cannot authorize retry mutations, and read-only reconciliation remains possible.

Impact:
Future Effect implementations must persist intent before mutation, reconcile observed state after ambiguous outcomes, bind mutation to a scoped Run and Worker capability, and keep Verification separate from executor success.

Status:
Active
```

```text
2026 08 08

Change:
Established Compensation as a durable execution lifecycle separate from the original Effect, with explicit capability, executor Evidence, independent Verification, and immutable Effect history.

Evidence:
The file Compensation suite proves restoration and removal, idempotent retry, post-effect drift refusal, cancellation, expired-grant read-only reconciliation, recovery after Evidence persistence failure, verifier rejection of later drift, and restart recovery. The CLI exercises the same path through azper undo.

Impact:
Future compensation mechanisms must reproduce staged state rather than invent inverse prompts, refuse to overwrite unrecognized state, retain the original Effect record, and require independent Verification before reporting Compensated.

Status:
Active
```

# 107. Final Engineering Test

Before completing any substantial Azper change, ask:

```text
Does this preserve the Contract as the source of user intent?

Does this preserve the Kernel as the source of execution truth?

Are external mutations represented as Effects?

Can success be independently verified?

Can failures recover without duplicating Effects?

Does memory preserve provenance and time?

Does the change work with local execution?

Does the change preserve model and runtime portability?

Are we reusing existing mechanisms instead of rebuilding them unnecessarily?

Would the TUI represent the resulting state truthfully?

Can a future engineer understand why this design exists?
```

If the answer to any important question is no, the implementation is not finished.

# 108. Azper Standard

Azper should win because it reliably accomplishes real work.

Not because it talks more.

Not because it exposes more tools.

Not because it uses more agents.

Not because it has more integrations.

The standard is:

```text
understand the intended outcome

maintain accurate state

choose capable execution resources

act with bounded authority

observe what actually happened

verify the requested outcome

recover when reality differs from the plan

learn only from evidence

remain inspectable and reversible
```

Every major subsystem should move Azper closer to that standard.
