#!/bin/sh
# Start release.yml for a tag semantic-release has just published.
#
# Forgejo raises no push event for a tag created with the automation token, so
# `on: push: tags` never fires for it — verified on this instance: v1.0.1 and
# v1.0.2 were both pushed by semantic-release and neither produced a run. The
# tag alone is therefore not enough to publish anything, and the release has to
# be asked for explicitly.
#
# semantic-release calls this from successCmd, which runs only when a version
# was actually released, so a push that warrants no release triggers nothing.
set -eu

TAG="${1:?usage: trigger-release.sh <tag>}"
: "${AUTOMATION_TOKEN:?AUTOMATION_TOKEN is not set}"
# main.yml sets these from the runner's own context; GITHUB_* is the same thing
# a step further down, and is what applies if this is ever run by hand outside a
# workflow. Nothing is hardcoded, so copying this script to another repository
# needs no edit.
BASE="${FORGEJO_BASE_URL:-${GITHUB_SERVER_URL:-}}"
REPO="${FORGEJO_REPO:-${GITHUB_REPOSITORY:-}}"
: "${BASE:?neither FORGEJO_BASE_URL nor GITHUB_SERVER_URL is set}"
: "${REPO:?neither FORGEJO_REPO nor GITHUB_REPOSITORY is set}"

CODE=$(curl -k -s -o /tmp/dispatch.out -w "%{http_code}" -X POST \
  -H "Authorization: token $AUTOMATION_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"ref\":\"main\",\"inputs\":{\"tag\":\"$TAG\"}}" \
  "$BASE/api/v1/repos/$REPO/actions/workflows/release.yml/dispatches")

echo "release.yml dispatch for $TAG: HTTP $CODE"
if [ "$CODE" != "204" ]; then
  cat /tmp/dispatch.out 2>/dev/null || true
  echo "ERROR: the tag is pushed but nothing will publish it."
  exit 1
fi
