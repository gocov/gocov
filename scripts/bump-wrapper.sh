#!/usr/bin/env bash
# The edit half of a wrapper bump: release.yml's bump-wrappers job clones
# a wrapper, runs this in the checkout, and commits and opens the PR from
# what it prints. Kept out of the workflow so it can be tested before a
# release runs it — scripts/bump-wrapper-test.sh runs it against the real
# wrappers on every CI run.
#
#   bump-wrapper.sh <action|pipe|component> <checkout> <gocov tag>
#
# Pins <gocov tag> in the wrapper and writes its release: the next minor
# after the wrapper's newest release tag, as the newest `## X.Y.Z` heading
# of CHANGELOG.md, which the wrapper's tag-on-release-merge tags (v-prefixed
# for the action). Prints "<old pin> <next version>", or nothing when the
# wrapper already pins the tag.
set -euo pipefail

usage() { echo "usage: $0 <action|pipe|component> <checkout> <gocov tag>" >&2; exit 2; }
[ $# -eq 3 ] || usage
kind=$1 dir=$2 tag=$3
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "not a gocov release tag: $tag" >&2; exit 2; }
cd "$dir"

# The pinned CLI, and the verb the changelog entry uses for it.
case $kind in
  action)
    verb=Pin
    old=$(sed -n 's/^ *default: *\(v[0-9][0-9.]*\) *$/\1/p' action.yml) ;;
  pipe)
    verb=Bake
    old=$(sed -n 's/^ARG GOCOV_VERSION=\(v[0-9][0-9.]*\) *$/\1/p' Dockerfile) ;;
  component)
    verb=Install
    old=$(sed -n 's/^ *default: *\(v[0-9][0-9.]*\) *$/\1/p' templates/upload.yml) ;;
  *) usage ;;
esac
[ -n "$old" ] || { echo "$kind: no pinned CLI version found" >&2; exit 1; }
[ "$old" != "$tag" ] || exit 0

# The next release: the minor after the newest release tag. Floating tags
# (the action's v1) are not versions and are skipped.
latest=$(git tag --list | sed 's/^v//' | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1)
[ -n "$latest" ] || { echo "$kind: no release tags found" >&2; exit 1; }
major=${latest%%.*} rest=${latest#*.}
next="$major.$(( ${rest%%.*} + 1 )).0"

case $kind in
  action)
    sed -i "s|^\( *default: *\)$old\$|\1$tag|" action.yml ;;
  pipe)
    sed -i "s|^ARG GOCOV_VERSION=$old\$|ARG GOCOV_VERSION=$tag|" Dockerfile
    # pipe.yml carries the version too, for the Atlassian catalog.
    sed -i "s|^image: gocov/upload-pipe:[0-9][0-9.]*\$|image: gocov/upload-pipe:$next|" pipe.yml ;;
  component)
    sed -i "s|^\( *default: *\)$old\$|\1$tag|" templates/upload.yml
    sed -i "s|\`$old\`|\`$tag\`|g" README.md ;;
esac

{ head -2 CHANGELOG.md
  printf '## %s\n\n- %s gocov CLI %s (was %s).\n\n' "$next" "$verb" "$tag" "$old"
  tail -n +3 CHANGELOG.md
} >CHANGELOG.new && mv CHANGELOG.new CHANGELOG.md

echo "$old $next"
