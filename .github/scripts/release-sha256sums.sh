#!/usr/bin/env bash
# Writes ./SHA256SUMS for the release workflow (.github/workflows/release.yml):
# the release's current SHA256SUMS with the lines of the given assets
# replaced by their sums here, every other line kept.
#
#   release-sha256sums.sh TAG ASSET...
#
# Run in the directory that holds the assets, with GH_TOKEN and GH_REPO set.
# A release or a SHA256SUMS that does not exist starts from an empty file;
# any other error from gh stops here, so a failed download never drops the
# lines of assets this job does not make.
set -euo pipefail

tag=$1
shift
[ "$#" -gt 0 ] || { echo "usage: $0 TAG ASSET..." >&2; exit 2; }

err=$(mktemp)
trap 'rm -f "$err"' EXIT
: > SHA256SUMS.old
if assets=$(gh release view "$tag" --json assets --jq '.assets[].name' 2>"$err"); then
  if grep -qx SHA256SUMS <<<"$assets"; then
    gh release download "$tag" --pattern SHA256SUMS --output SHA256SUMS.old --clobber
  fi
elif grep -q 'release not found' "$err"; then
  echo "release $tag does not exist: SHA256SUMS starts empty" >&2
else
  cat "$err" >&2
  exit 1
fi

awk 'NR == FNR { ours[$0]; next } !($2 in ours)' <(printf '%s\n' "$@") SHA256SUMS.old > SHA256SUMS.keep
sha256sum "$@" >> SHA256SUMS.keep
sort -k2 SHA256SUMS.keep > SHA256SUMS
rm -f SHA256SUMS.old SHA256SUMS.keep
cat SHA256SUMS
