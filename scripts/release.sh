#!/usr/bin/env bash
set -euo pipefail

fail() { echo "Error: $*" >&2; exit 1; }
RELEASE_TYPE=${1:-}
MODULES=${2:-}
[[ "$RELEASE_TYPE" == break || "$RELEASE_TYPE" == patch ]] || fail "Usage: make release-patch OR make release-break"
[[ -f go.mod ]] || fail "Run from the repository root."
[[ -n "$MODULES" ]] || fail "MODULES is empty."
REPO=$(git rev-parse --show-toplevel)
[[ "$(pwd -P)" == "$(cd "$REPO" && pwd -P)" ]] || fail "Run from the repository root."
[[ -z "$(git status --porcelain --untracked-files=all)" ]] || fail "Commit or stash tracked and untracked changes first."
SOURCE=$(git rev-parse HEAD)
ROOT_MODULE=$(awk '$1 == "module" {print $2; exit}' go.mod)
[[ -n "$ROOT_MODULE" ]] || fail "No root module path."
FETCH_URL=$(git remote get-url origin)
PUSH_URL=$(git remote get-url --push origin)
# Local relative remote paths must remain relative to the original checkout.
if [[ -d "$FETCH_URL" ]]; then FETCH_URL=$(cd "$FETCH_URL" && pwd); fi
if [[ -d "$PUSH_URL" ]]; then PUSH_URL=$(cd "$PUSH_URL" && pwd); fi

MANIFESTS=()
TAGS=()
ROOT_FOUND=false
for dir in $MODULES; do
    dir=${dir#./}
    [[ "$dir" == . || "$dir" =~ ^[a-zA-Z0-9_-]+(/[a-zA-Z0-9_-]+)*$ ]] || fail "Invalid module directory: $dir"
    [[ "$dir" != integration && "$dir" != integration/* ]] || fail "Integration fixtures are not release modules."
    [[ -f "$dir/go.mod" ]] || fail "Missing manifest: $dir/go.mod"
    for manifest in "${MANIFESTS[@]+${MANIFESTS[@]}}"; do
        [[ "$manifest" != "$dir/go.mod" ]] || fail "Duplicate module: $dir"
    done
    expected="$ROOT_MODULE/$dir"
    if [[ "$dir" == . ]]; then expected=$ROOT_MODULE; ROOT_FOUND=true; fi
    actual=$(awk '$1 == "module" {print $2; exit}' "$dir/go.mod")
    [[ "$actual" == "$expected" ]] || fail "Unexpected module identity in $dir/go.mod"
    MANIFESTS+=("$dir/go.mod")
done
$ROOT_FOUND || fail "Root module must be included."

STAGING=$(mktemp -d "${TMPDIR:-/tmp}/metry-release.XXXXXX")
cleanup() {
    result=$?
    trap - EXIT
    rm -rf "$STAGING"
    if [[ $result != 0 ]]; then
        echo "Release failed; original checkout and local tags were not changed. Inspect origin tags before retrying." >&2
    fi
    exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# Clone isolates the index, HEAD, tags and manifest edits from the development repo.
git clone --quiet --no-hardlinks --no-checkout "$REPO" "$STAGING/repo"
git -C "$STAGING/repo" remote set-url origin "$FETCH_URL"
git -C "$STAGING/repo" remote set-url --push origin "$PUSH_URL"
# Preserve effective signing policy, including repository-local SSH/GPG programs.
while read -r key value; do
    git -C "$STAGING/repo" config "$key" "$value"
done < <(git config --get-regexp '^(user\.(name|email|signingkey)|commit\.gpgsign|tag\.gpgsign|gpg\..*)$' || true)
cd "$STAGING/repo"
git checkout --quiet --detach "$SOURCE"
git fetch --quiet --tags origin
LATEST=$(git tag -l 'v*' | sed -nE 's/^v([0-9]+\.[0-9]+\.[0-9]+)$/\1/p' | sort -t. -k1,1n -k2,2n -k3,3n | tail -n 1)
LATEST=${LATEST:-0.0.0}
IFS=. read -r MAJOR MINOR PATCH <<< "$LATEST"
MAJOR=$((10#$MAJOR)); MINOR=$((10#$MINOR)); PATCH=$((10#$PATCH))
if [[ "$RELEASE_TYPE" == break ]]; then
    if [[ $MAJOR == 0 ]]; then MINOR=$((MINOR + 1)); else MAJOR=$((MAJOR + 1)); MINOR=0; fi
    PATCH=0
else
    PATCH=$((PATCH + 1))
fi
NEW_VERSION="v$MAJOR.$MINOR.$PATCH"
for dir in $MODULES; do
    dir=${dir#./}
    tag=$NEW_VERSION
    if [[ "$dir" != . ]]; then tag="$dir/$NEW_VERSION"; fi
    git show-ref --verify --quiet "refs/tags/$tag" && fail "Tag already exists: $tag"
    TAGS+=("$tag")
done
printf 'Current version: v%s\nNew version: %s (%s)\n' "$LATEST" "$NEW_VERSION" "$RELEASE_TYPE"
read -r -p "Proceed with release $NEW_VERSION? [y/N] " REPLY || fail "No confirmation."
[[ "$REPLY" == y || "$REPLY" == Y ]] || fail "Aborted."

for manifest in "${MANIFESTS[@]}"; do
    # Inspect both block and single-line directives; Go owns manifest rewriting.
    awk -v root="$ROOT_MODULE" '
        $1 == "require" && $2 == "(" { block="require"; next }
        $1 == "replace" && $2 == "(" { block="replace"; next }
        $1 == ")" { block=""; next }
        {
            kind=block; path=$1; version=$2; arrow=$2
            if ($1 == "require" || $1 == "replace") { kind=$1; path=$2; version=$3; arrow=$3 }
            if (path != root && index(path, root "/") != 1) next
            if (kind == "require") print "require", path
            if (kind == "replace") {
                if (arrow == "=>") print "replace", path
                else print "replace", path "@" version
            }
        }
    ' "$manifest" > "$STAGING/edits"
    while read -r kind path; do
        if [[ "$kind" == require ]]; then
            GOWORK=off go mod edit "-require=$path@$NEW_VERSION" "$manifest"
        else
            GOWORK=off go mod edit "-dropreplace=$path" "$manifest"
        fi
    done < "$STAGING/edits"
    GOWORK=off go mod edit -fmt "$manifest"
done
# Only validated release manifests can enter this commit.
git add -- "${MANIFESTS[@]}"
if ! git diff --cached --quiet; then git commit --quiet -m "chore: release $NEW_VERSION"; fi
for tag in "${TAGS[@]}"; do
    if [[ "$(git config --bool --get tag.gpgsign || true)" == true ]]; then
        git tag -m "Release $NEW_VERSION" "$tag"
    else
        git tag "$tag"
    fi
done
REFS=()
for tag in "${TAGS[@]}"; do REFS+=("refs/tags/$tag:refs/tags/$tag"); done
# All-or-nothing, explicit refs: unrelated local tags never leave this checkout.
git push --atomic origin "${REFS[@]}"
echo "Release $NEW_VERSION published. Original checkout is unchanged."
