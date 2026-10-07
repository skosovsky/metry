#!/usr/bin/env bash
# AAA regression scenarios; uses only temporary local repositories and bare remotes.
set -euo pipefail
# Never inherit process-level remote/credential overrides into synthetic repos.
# Even an accidental remote URL must not permit network publication from fixtures.
unset GIT_CONFIG_COUNT GIT_CONFIG_PARAMETERS GIT_CONFIG GIT_CONFIG_SYSTEM
export GIT_ALLOW_PROTOCOL=file
SCRIPT=$(cd "$(dirname "$0")" && pwd)/release.sh
SUITE=$(mktemp -d "${TMPDIR:-/tmp}/metry-release-tests.XXXXXX")
trap 'rm -rf "$SUITE"' EXIT
export GIT_CONFIG_GLOBAL="$SUITE/gitconfig"
export GIT_CONFIG_NOSYSTEM=1
export GIT_TERMINAL_PROMPT=0
cat > "$GIT_CONFIG_GLOBAL" <<'CONFIG'
[user]
    name = Release Test
    email = release@example.invalid
[commit]
    gpgsign = false
[tag]
    gpgsign = false
CONFIG
fail() { echo "FAIL: $*" >&2; exit 1; }
fixture() {
    CASE="$SUITE/$1"
    mkdir -p "$CASE/repo/middleware/grpc" "$CASE/repo/integration/otlp" "$CASE/repo/scripts"
    git init --quiet --bare "$CASE/remote.git"
    git init --quiet "$CASE/repo"
    cp "$SCRIPT" "$CASE/repo/scripts/release.sh"
    cat > "$CASE/repo/go.mod" <<'MOD'
module example.com/metry

go 1.20
MOD
    cat > "$CASE/repo/middleware/grpc/go.mod" <<'MOD'
module example.com/metry/middleware/grpc

go 1.20

require example.com/metry v0.0.0

replace (
    example.com/metry => ../..
    example.com/metry v0.0.0 => ../..
)
MOD
    cat > "$CASE/repo/integration/otlp/go.mod" <<'MOD'
module example.com/metry/integration/otlp

go 1.20
MOD
    cd "$CASE/repo"
    git add .
    git commit --quiet -m 'initial'
    git tag v0.1.0
    git tag unrelated-local-tag
    git remote add origin "$CASE/remote.git"
    git push --quiet origin HEAD refs/tags/v0.1.0
    BEFORE=$(git rev-parse HEAD)
    BRANCH=$(git symbolic-ref HEAD)
}
release() { printf 'y\n' | "$BASH" scripts/release.sh patch '. ./middleware/grpc'; }
unchanged() {
    [[ "$(git rev-parse HEAD)" == "$BEFORE" ]] || fail 'HEAD changed'
    [[ "$(git symbolic-ref HEAD)" == "$BRANCH" ]] || fail 'Branch changed'
    [[ -z "$(git status --porcelain --untracked-files=all)" ]] || fail 'Checkout dirty'
    ! git show-ref --verify --quiet refs/tags/v0.1.1 || fail 'Release tag leaked locally'
}

# Arrange: clean repo with unrelated local tag and development replacements.
fixture success
# Act.
release > "$CASE/output" 2>&1 || { cat "$CASE/output"; fail 'Release failed'; }
# Assert: only expected tags and release manifests, no fixture or unrelated tag.
unchanged
[[ "$(git --git-dir="$CASE/remote.git" tag -l | sort)" == "$(printf 'middleware/grpc/v0.1.1\nv0.1.0\nv0.1.1')" ]] || fail 'Unexpected remote tags'
ROOT_REF=$(git --git-dir="$CASE/remote.git" rev-parse v0.1.1)
[[ "$ROOT_REF" == "$(git --git-dir="$CASE/remote.git" rev-parse middleware/grpc/v0.1.1)" ]] || fail 'Module tag mismatch'
[[ "$(git --git-dir="$CASE/remote.git" diff-tree --no-commit-id --name-only -r "$ROOT_REF")" == middleware/grpc/go.mod ]] || fail 'Unexpected staged file'
RELEASE_MOD=$(git --git-dir="$CASE/remote.git" show v0.1.1:middleware/grpc/go.mod)
[[ "$RELEASE_MOD" == *'require example.com/metry v0.1.1'* ]] || fail 'Dependency not updated'
[[ "$RELEASE_MOD" != *replace* ]] || fail 'Development replacements retained'

# Arrange: v0 breaking release and a root-only manifest that needs no edits.
fixture break
# Act.
printf 'y\n' | "$BASH" scripts/release.sh break '.' > "$CASE/output" 2>&1 || { cat "$CASE/output"; fail 'Break release failed'; }
# Assert.
unchanged
[[ "$(git --git-dir="$CASE/remote.git" rev-parse v0.2.0)" == "$BEFORE" ]] || fail 'Root-only tag should use source HEAD'

# Arrange: operator declines the proposed release.
fixture cancelled
# Act.
if printf 'n\n' | "$BASH" scripts/release.sh patch '.' > "$CASE/output" 2>&1; then fail 'Ignored cancellation'; fi
# Assert.
unchanged
[[ "$(git --git-dir="$CASE/remote.git" tag -l)" == v0.1.0 ]] || fail 'Cancelled release published tags'

# Arrange: unrelated untracked content.
fixture untracked
printf 'private content\n' > untracked-private-note.txt
# Act.
if release > "$CASE/output" 2>&1; then fail 'Accepted untracked file'; fi
# Assert.
[[ "$(git rev-parse HEAD)" == "$BEFORE" ]] || fail 'Rejected checkout changed'
[[ "$(cat untracked-private-note.txt)" == 'private content' ]] || fail 'Untracked file changed'
[[ "$(git --git-dir="$CASE/remote.git" tag -l)" == v0.1.0 ]] || fail 'Rejected release pushed tags'
rm untracked-private-note.txt
unchanged

# Arrange: tracked and staged changes each fail clean-state preflight.
fixture dirty
printf '\n// changed\n' >> go.mod
if release > "$CASE/output" 2>&1; then fail 'Accepted tracked change'; fi
[[ "$(git rev-parse HEAD)" == "$BEFORE" ]] || fail 'Dirty checkout changed'
git add go.mod
if release > "$CASE/output" 2>&1; then fail 'Accepted staged change'; fi
git restore --staged go.mod
git restore go.mod
unchanged

# Arrange: remote rejects one tag; atomic push must publish neither tag.
fixture rejected-push
cat > "$CASE/remote.git/hooks/update" <<'HOOK'
#!/usr/bin/env bash
[[ "$1" != refs/tags/middleware/grpc/v0.1.1 ]]
HOOK
chmod +x "$CASE/remote.git/hooks/update"
# Act.
if release > "$CASE/output" 2>&1; then fail 'Expected remote rejection'; fi
# Assert.
unchanged
[[ "$(git --git-dir="$CASE/remote.git" tag -l)" == v0.1.0 ]] || fail 'Atomic push published partial tags'

# Arrange: malformed manifest causes edit failure after entering the isolated checkout.
fixture malformed
printf '\ninvalid directive\n' >> middleware/grpc/go.mod
git add middleware/grpc/go.mod
git commit --quiet -m 'malformed manifest'
BEFORE=$(git rev-parse HEAD)
# Act.
if release > "$CASE/output" 2>&1; then fail 'Accepted malformed manifest'; fi
# Assert.
unchanged
[[ "$(git --git-dir="$CASE/remote.git" tag -l)" == v0.1.0 ]] || fail 'Failed edit pushed tags'

# Arrange: fixtures must not be treated as release modules.
fixture integration
# Act.
if printf 'y\n' | "$BASH" scripts/release.sh patch '. ./integration/otlp' > "$CASE/output" 2>&1; then fail 'Published fixture'; fi
# Assert.
unchanged

# Arrange: repository-local tag signing policy with an unavailable signer.
fixture signing-tag
git config tag.gpgsign true
git config gpg.format openpgp
git config gpg.program /definitely/missing/signer
# Act: root-only release needs a tag, but no new commit.
if printf 'y\n' | "$BASH" scripts/release.sh patch '.' > "$CASE/output" 2>&1; then fail 'Bypassed tag signing policy'; fi
# Assert.
unchanged
[[ "$(git --git-dir="$CASE/remote.git" tag -l)" == v0.1.0 ]] || fail 'Signing failure published tags'

# Arrange: commit signing fails while preparing changed manifests.
fixture signing-commit
git config commit.gpgsign true
git config gpg.format ssh
git config user.signingkey /definitely/missing/key
git config gpg.ssh.program /definitely/missing/signer
# Act.
if release > "$CASE/output" 2>&1; then fail 'Bypassed commit signing policy'; fi
# Assert.
unchanged
[[ "$(git --git-dir="$CASE/remote.git" tag -l)" == v0.1.0 ]] || fail 'Commit signing failure published tags'
printf 'PASS: isolated release, manifests, tags, clean state and failure cleanup\n'
