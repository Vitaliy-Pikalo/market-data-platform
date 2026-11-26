# CLAUDE.md

Rules for every Claude Code session in this repo. They apply to every change, however small.
Human contributors follow the same rules; see [CONTRIBUTING.md](CONTRIBUTING.md).

## Branches

- Never commit to `main`. All changes reach `main` through a pull request.
- One branch per issue, named `<type>/<issue#>-<slug>`, for example `feat/12-coinbase-recorder` or `fix/31-kraken-reconnect`.
  - `<type>` is one of the conventional-commit types below.
  - If there is no issue yet, create one first (`gh issue create`) so the branch has a number.

## Commits

- Conventional Commits: `<type>(<optional scope>): <summary>`, where type is one of
  `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `perf`, `build`, `ci`.
- One logical change per commit. If the message needs "and", it is probably two commits.
- Every commit must build and pass tests on its own: `go build ./... && go test ./...`.
  (While the module has no Go packages, `go test ./...` exits 1 with "no packages to test"; until the first package lands, `go build ./...` is the gate.)
- No filler commits. A formatting-only or whitespace-only commit is allowed only when it is a real change
  (for example applying `gofmt` to code that was not formatted), never to pad history.

## Pull requests

- Every PR uses [.github/pull_request_template.md](.github/pull_request_template.md) and fills in every section.
- The PR body links its issue with `Closes #N`.
- Every claim needs evidence: the exact command and its output (tests pass, latency is X, no data lost, and so on).
  A claim without evidence does not go in the PR.

## Numbers in the README

- A number goes in the README **Results** section only after it has been reproduced twice on Linux.
  Record the command, the commit SHA and both runs next to it.
- Numbers from Windows/macOS or a single run may go in a PR or issue as preliminary, labelled as such.

## Before pushing

- Stop and show the user the diff (`git diff origin/main...HEAD` plus `git log --oneline origin/main..HEAD`) before any `git push`.
  Push only after the user approves.
- Never merge a PR; the user merges.

## Enforcement

The push and merge rules above are enforced by Claude Code permissions in
[.claude/settings.json](.claude/settings.json), not only by these instructions:

| Rule | Setting |
|---|---|
| Merging is always done by the human | `deny`: `gh pr merge`, `gh pr ready` |
| No force pushes | `deny`: `git push --force`, `git push -f` |
| Stop and show the diff before pushing | `ask`: `git push` |
| The human approves opening a PR | `ask`: `gh pr create` |

- Merging is always done by the human. Do not try to get around a denied command (other tools, the GitHub API, a script).
- The rules match by command prefix. `git push origin main --force` is not caught by the force-push deny rule,
  but it still needs approval through the `git push` ask rule. Never force push.
- Personal overrides go in `.claude/settings.local.json`, which is gitignored. Do not weaken these rules in the shared file.
