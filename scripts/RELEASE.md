# Release workflow

Run `make release-patch` or `make release-break` from a clean repository root.
Tracked, staged and non-ignored untracked files must all be committed or stashed.
The Makefile first runs lint, tests and the isolated release regression suite.
Integration fixtures participate in tests but are never published modules.

The script clones the current HEAD into a temporary checkout, fetches origin tags
there and calculates the next version. Patch increments patch; break increments
minor for v0 or major for v1+. Confirmation names the version. Repository paths
may contain spaces; module directory names must use letters, digits, underscores,
hyphens and slash separators. Root module must be included exactly once.

Go's manifest editor updates internal dependency versions and removes internal
development replacements in the explicit release module manifests. Only those
go.mod paths are staged. A release commit is created only if manifests changed.
Root and module tags name the same release commit. Only tags created by this run
are pushed, in one atomic push; origin must support atomic pushes. The development
checkout, index, branch and local tags remain unchanged throughout the release.
Signing configuration is honored; signing failure aborts the isolated release.

The workflow uses Bash 3.2+, Git, Go and portable awk/sed/sort utilities on macOS
and Linux. No BSD-only in-place sed option or GNU version-sort extension is used.
`make test-release` exercises a temporary local repo and bare remote without
network publication. macOS Bash 3.2 and current Bash can run the same suite;
the Linux CI job runs it as well. A local macOS pass alone is not Linux evidence.

On failure or cancellation the temporary checkout is removed. No development
checkout recovery commands are needed. On a rejected atomic push neither release
tag is published. If the connection drops while the remote accepts a push, inspect
`git ls-remote --tags origin` before retrying: remote publication may have succeeded
despite a local error. All run-created tags must exist at the same commit to count
as a published release; do not delete remote tags to hide an ambiguous outcome.
Resolve the transport/signing/manifest error, then rerun from the unchanged source
HEAD. Local tags are intentionally not imported after release; fetch them when
needed. Uncatchable process termination can leave only a `metry-release.*`
temporary directory, which can be removed after verifying no release process is
using it.
