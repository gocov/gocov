#!/usr/bin/env bash
# Tests scripts/bump-wrapper.sh against the real wrappers, as they are on
# their main branches today — the half of a release that otherwise runs
# for the first time during the release. For each wrapper:
#
#   - a bump to a new CLI prints the old pin and the next version (the
#     minor after the newest release tag), writes that version as the
#     newest CHANGELOG.md heading, and leaves the wrapper passing its own
#     scripts/check-pins.sh;
#   - the wrapper's own tag-on-release-merge step, run on the result,
#     reads the tag this script meant (the contract between the two
#     repos), and that tag does not exist yet;
#   - a bump to the CLI already pinned prints nothing and changes nothing.
#
# Needs network access to github.com; ci.yml's workflows job runs it.
set -euo pipefail

cd "$(dirname "$0")/.."
bump=$PWD/scripts/bump-wrapper.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fails=0
fail() { echo "FAIL: $*"; fails=$((fails + 1)); }

# The step the wrapper's tag-on-release-merge runs to read its version,
# run as-is from its workflow file; prints the tag it would push.
tag_step() {
  python3 - "$1/.github/workflows/tag-on-release-merge.yml" >"$work/step.sh" <<'PY'
import sys, yaml
wf = yaml.safe_load(open(sys.argv[1]))
steps = [s for job in wf["jobs"].values() for s in job.get("steps", [])]
print(next(s["run"] for s in steps if s.get("name") == "Read the version CHANGELOG.md releases"))
PY
  : >"$work/out"
  (cd "$1" && GITHUB_OUTPUT="$work/out" bash -e "$work/step.sh" >/dev/null) || return 1
  sed -n 's/^tag=//p' "$work/out"
}

for spec in action:gocov-action pipe:upload-pipe component:gitlab-component; do
  kind=${spec%%:*} repo=${spec#*:}
  echo "--- $repo"
  before=$fails
  src="$work/$kind.src"
  git clone -q "https://github.com/gocov/$repo" "$src"
  case $kind in
    pipe) pin=$(sed -n 's/^ARG GOCOV_VERSION=\(v[0-9][0-9.]*\) *$/\1/p' "$src/Dockerfile") ;;
    action) pin=$(sed -n 's/^ *default: *\(v[0-9][0-9.]*\) *$/\1/p' "$src/action.yml") ;;
    component) pin=$(sed -n 's/^ *default: *\(v[0-9][0-9.]*\) *$/\1/p' "$src/templates/upload.yml") ;;
  esac
  latest=$(git -C "$src" tag --list | sed 's/^v//' | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1)
  major=${latest%%.*} rest=${latest#*.}
  want="$major.$(( ${rest%%.*} + 1 )).0"
  prefix=; [ "$kind" = action ] && prefix=v

  # A bump to a CLI release newer than any real one.
  d="$work/$kind.bump"
  git clone -q "$src" "$d"
  out=$("$bump" "$kind" "$d" v99.0.0) || { fail "$repo: bump-wrapper.sh failed"; continue; }
  [ "$out" = "$pin $want" ] || fail "$repo: printed '$out', want '$pin $want'"
  head=$(sed -n 's/^## \([0-9][0-9.]*\) *$/\1/p' "$d/CHANGELOG.md" | head -1)
  [ "$head" = "$want" ] || fail "$repo: newest CHANGELOG.md heading is '$head', want '$want'"
  grep -qF "gocov CLI v99.0.0 (was $pin)." "$d/CHANGELOG.md" || fail "$repo: no changelog line for v99.0.0"
  if [ "$kind" = pipe ]; then
    grep -qx "image: gocov/upload-pipe:$want" "$d/pipe.yml" || fail "$repo: pipe.yml does not carry $want"
  fi
  (cd "$d" && bash scripts/check-pins.sh >/dev/null) || fail "$repo: its check-pins.sh rejects the bump"
  tag=$(tag_step "$d") || { fail "$repo: its tag-on-release-merge step rejects the bump"; tag=; }
  [ "$tag" = "$prefix$want" ] || fail "$repo: its tag-on-release-merge step would tag '$tag', want '$prefix$want'"

  # A bump to the CLI it already pins.
  d="$work/$kind.same"
  git clone -q "$src" "$d"
  out=$("$bump" "$kind" "$d" "$pin") || fail "$repo: bump-wrapper.sh failed on the current pin"
  [ -z "$out" ] || fail "$repo: printed '$out' for the CLI it already pins"
  [ -z "$(git -C "$d" status --porcelain)" ] || fail "$repo: changed files for the CLI it already pins"

  [ "$fails" -ne "$before" ] || echo "ok: $pin -> v99.0.0 releases $prefix$want"
done

[ "$fails" -eq 0 ] || { echo "$fails failure(s)"; exit 1; }
echo "bump-wrapper.sh: all wrappers ok"
