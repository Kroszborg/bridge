#!/usr/bin/env bash
# Prints the CHANGELOG.md section for one version, e.g. `release-notes.sh 0.3.0`.
# Fails when the section is missing, so a release never ships without notes.
set -euo pipefail

version="${1:?usage: release-notes.sh <version>}"
changelog="${2:-CHANGELOG.md}"

notes="$(awk -v v="$version" '
  /^## \[/ { if (found) exit; found = index($0, "## [" v "]") == 1; next }
  found
' "$changelog")"

if [ -z "$(printf '%s' "$notes" | tr -d '[:space:]')" ]; then
  echo "CHANGELOG.md has no section for $version. Rename [Unreleased] to [$version] - $(date +%F) first." >&2
  exit 1
fi

printf '%s\n' "$notes"
