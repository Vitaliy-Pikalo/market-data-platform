# 0001. Record architecture decisions

- **Status:** Accepted
- **Date:** 2026-11-26
- **Issue/PR:** —

## Context

This project makes many design choices whose reasons are easy to lose: Kafka replication settings,
ClickHouse table engines and ordering keys, how ingestors deduplicate and detect gaps, how backups and
restores work. Months later, neither a reviewer nor the author can tell from the code alone why a choice
was made, what else was considered, or whether it is still valid. Commit messages and PR descriptions
are too scattered to answer that.

## Decision

We will record architecturally significant decisions as Architecture Decision Records (ADRs), in the format
described by Michael Nygard, stored in `docs/adr/` as `NNNN-short-title.md` and numbered sequentially.

- New ADRs start from [0000-template.md](0000-template.md) (context, decision, alternatives, consequences).
- An ADR is added in the same PR as the change it describes, or before it.
- Accepted ADRs are not rewritten. A changed decision gets a new ADR that supersedes the old one,
  and the old one's status is updated to point to it.

## Alternatives

### No formal records; rely on commits, PRs and the README

- Pros: no extra process.
- Cons: reasoning is scattered and hard to find; rejected alternatives are not recorded at all.

### A single design document

- Pros: one place to read.
- Cons: tends to be rewritten in place, so the history of *why* a decision changed is lost; grows unwieldy.

## Consequences

- Each significant decision costs a short document, written while the reasoning is fresh.
- Reviewers and future contributors can find why the system looks the way it does, and what would justify changing it.
- Some judgement is needed about what counts as "significant"; when in doubt, write the ADR.
