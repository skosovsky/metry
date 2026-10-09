# Shell release

The Makefile and Bash release protocol follow ragy commit
`91e3ff2cdc87c49b5ad9d1cf0afa23d35832521e`. They use Bash, Git, Make and Go directly.

```sh
make lint
make test
make test-integration
make test-e2e
make release-patch
make release-break
make release-patch RELEASE_SOURCE=<full-commit-sha>
make release-inspect
make release-resume
make release-finish
```

The caller must be on local `main` with clean tracked files and index. Untracked
files are preserved and excluded. Source defaults to HEAD; an explicit full SHA
must be an ancestor of local main. Origin must have exactly one push destination
and an existing main that can fast-forward to source. An older source cannot rewind
remote main. The caller's branch, HEAD, files and tags remain unchanged.

Versions come from remote root tags. Patch increments patch; break increments
minor before v1. A v1-to-v2 transition requires an import-path migration and is
blocked. The source's `make modules` discovers every module; all are published:
root `vX.Y.Z`, `middleware/grpc/vX.Y.Z` and `integration/otlp/vX.Y.Z` in the current
layout. Each nested module path must equal the root module path plus its directory.
Future modules need no manually updated release inventory.

The isolated source checkout runs lint, fresh unit tests, integration and e2e in
order. Missing prerequisites or failed gates prevent publication. Development
replacements remain in source/main. Only the candidate receives matching internal
requirements and removal of internal replacements; its commit changes only module
`go.mod`/`go.sum` manifests. No fabricated checksum entries are added.

After showing exact source, candidate, destination and refs for confirmation,
one atomic push delivers source to `refs/heads/main` and candidate to every exact
module tag. No force, broad `--tags` or non-atomic fallback is used. Remote main is
rechecked; a concurrent incompatible update rejects publication. Success requires
exact candidate tag identities and remote main equal to or descended from source.
Consumer/proxy checks are source integration tests, not post-push polling.

## Recovery

Format-3 plain-text state lives in `library-releases/active` under the Git common
directory, recording destination, branch, remote main, source, candidate, version,
phase and publication status. State is never evaluated as shell code. A local lock
prevents simultaneous releases; check that its owner is gone before removing a
stale lock after process termination.

`inspect` refreshes remote observations. `resume` retains version and candidate;
unknown results require inspection first. Lost push responses are resolved by
reading exact refs. Later forward movement of main is allowed after source delivery
is verified; different tags are collisions. Recovery never rewinds main or changes
public tags. `finish` archives only a verified publication. Legacy active records
or unsupported formats block publication; recover them with their original tooling.

## Verification

Ordinary Go tests in `scripts/` exercise disposable bare repositories: source/main,
prepared candidate/tags, failed gates, atomic rejection, remote races, candidate
retention and inspect/resume/finish. User transport and signing config are isolated;
network Git publication is forbidden in fixtures. Integration tests verify current
source artifacts and external semantic consumers. Tests never publish to origin.
Supported verification platforms are macOS and Linux with Bash 3.2 or later.
Actual platform/tool results belong in the task report, not an assumed PASS.
See [verification](../verification.md) for runtime prerequisites and profiles.
