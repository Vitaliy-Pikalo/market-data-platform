# Contributing

Thanks for helping. These rules keep the history readable and every result reproducible.
Claude Code sessions follow the same rules via [CLAUDE.md](CLAUDE.md).

## Workflow

1. **Open or pick an issue.** Use the *Task* or *Bug* issue template. Every change starts from an issue.
2. **Branch from `main`**, named `<type>/<issue#>-<slug>`:
   ```sh
   git switch main && git pull
   git switch -c feat/12-coinbase-recorder
   ```
   Never commit directly to `main`; it is protected and only accepts pull requests.
3. **Commit** using [Conventional Commits](https://www.conventionalcommits.org/):
   `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `perf`, `build`, `ci`.
   - One logical change per commit.
   - Each commit must build and pass `go test ./...` on its own, so `git bisect` always works.
   - No filler commits: formatting- or whitespace-only commits are fine only when they are a real change.
4. **Open a pull request** with the template. Fill in every section, link the issue with `Closes #N`,
   and back every claim with the command you ran and its output.
5. **Review and merge.** The maintainer merges.

## Results and benchmarks

Numbers go in the README **Results** section only after they have been reproduced twice on Linux.
Include the command, the commit SHA and both runs. Single runs or non-Linux numbers can be shared in
issues or PRs, marked as preliminary.

## Merge strategy

- PRs are merged with a **merge commit**. Squash merging and rebase merging are disabled.
  This keeps each reviewed commit in history and keeps the PR boundary visible.
- Because commits are kept as-is, clean up your branch (`git rebase -i main`, fixups) before requesting review.
- The branch is deleted automatically after merge.

## Releases and tags

Each completed project phase is tagged on `main` as `v0.N.0-phaseN`, where `N` is the phase number:

| Phase | Tag |
|---|---|
| 0: Setup | `v0.0.0-phase0` |
| 1: Ingest | `v0.1.0-phase1` |
| … | … |
| 6: DR + chaos | `v0.6.0-phase6` |

A phase is tagged only when its "Done when" criterion in the README is met with evidence.
Before tagging, move the
`Unreleased` entries in [CHANGELOG.md](CHANGELOG.md) under the new version.

```sh
git tag -a v0.1.0-phase1 -m "Phase 1: Ingest"
git push origin v0.1.0-phase1
```

## Architecture decisions

Significant design choices are recorded as ADRs in [docs/adr/](docs/adr/). Copy
[0000-template.md](docs/adr/0000-template.md) to the next free number and open it in the same PR as the change.

## Incidents

Every incident gets a postmortem in [docs/postmortems/](docs/postmortems/) (see `TEMPLATE.md`) and an issue labelled `incident`.
