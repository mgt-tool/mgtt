#!/bin/sh
# Copyright (C) 2026 Alex Kunich
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# One version, said in three places, checked in one.
#
#   VERSION        what the binary prints, what the image is tagged with
#   CHANGELOG.md   a "## [X.Y.Z] — YYYY-MM-DD" section for that version
#   the git tag    vX.Y.Z, and only ever on a commit where the first two agree
#
# Two modes.  With no argument this is the everyday check (CI on every push):
# VERSION must be a plain semver and CHANGELOG.md must know it.  With a tag
# name it is the release check: the tag must spell VERSION, the section must
# carry a date, and [Unreleased] must be empty -- a release that ships work
# the changelog calls unreleased is a release the changelog lies about.
#
# Exit 0 clean, 1 with every disagreement named.
set -eu

cd "$(dirname "$0")/.."

version=$(tr -d '[:space:]' < VERSION)
tag=${1:-}
fail=0
say() { echo "release-check: $*" >&2; fail=1; }

case "$version" in
  [0-9]*.[0-9]*.[0-9]*) ;;
  *) say "VERSION is '$version'; a release version is X.Y.Z with no prefix" ;;
esac

# The section heading, exactly as Keep a Changelog spells it here.
heading=$(grep -E "^## \[$version\]" CHANGELOG.md || true)
if [ -z "$heading" ]; then
  say "CHANGELOG.md has no '## [$version]' section"
fi

if [ -n "$tag" ]; then
  [ "$tag" = "v$version" ] || say "tag is '$tag' but VERSION is '$version'; tag v$version, or set VERSION first"

  if [ -n "$heading" ]; then
    echo "$heading" | grep -qE "^## \[$version\] — [0-9]{4}-[0-9]{2}-[0-9]{2}$" \
      || say "the '[$version]' section has no date; a released section reads '## [$version] — YYYY-MM-DD'"
  fi

  # Everything between "## [Unreleased]" and the next "## " must be blank.
  unreleased=$(awk '/^## \[Unreleased\]/{on=1; next} /^## /{on=0} on && NF' CHANGELOG.md || true)
  if [ -n "$unreleased" ]; then
    say "[Unreleased] is not empty; move its entries under '## [$version] — <date>' before tagging:"
    echo "$unreleased" | head -5 | sed 's/^/    /' >&2
  fi
fi

[ "$fail" -eq 0 ] && echo "release-check: $version${tag:+ ($tag)} ok"
exit "$fail"
