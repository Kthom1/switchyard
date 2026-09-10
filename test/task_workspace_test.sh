#!/usr/bin/env bash
set -euo pipefail
hook=$(realpath "$(dirname "$0")/../scripts/task-workspace")
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
git init -q "$scratch/source"
git -C "$scratch/source" -c user.name=Test -c user.email=test@example.invalid commit -qm initial --allow-empty
branch=$(git -C "$scratch/source" branch --show-current)
git -C "$scratch/source" config credential.helper original
mkdir "$scratch/source/failed"
if (cd "$scratch/source/failed"; SOURCE_REPO_URL="$scratch/missing" "$hook" init) 2>/dev/null; then
  echo 'Failed clone unexpectedly succeeded'; exit 1
fi
[[ $(git -C "$scratch/source" branch --show-current) = "$branch" ]]
[[ $(git -C "$scratch/source" config --local --get-all credential.helper) = original ]]
if SYMPHONY_WORKSPACE_ROOT="$scratch/source/nested" "$hook" root 2>/dev/null; then exit 1; fi
SYMPHONY_WORKSPACE_ROOT="$scratch/tasks" "$hook" root
mkdir "$scratch/tasks/TEST-1"
(cd "$scratch/tasks/TEST-1"; SOURCE_REPO_URL="$scratch/source" "$hook" init; SOURCE_REPO_URL="$scratch/source" "$hook" check)
if (cd "$scratch/tasks/TEST-1"; SOURCE_REPO_URL="$scratch/other" "$hook" check) 2>/dev/null; then exit 1; fi
mkdir "$scratch/source/reused"
if (cd "$scratch/source/reused"; SOURCE_REPO_URL="$scratch/source" "$hook" check) 2>/dev/null; then exit 1; fi
echo 'Workspace clone failure, root, origin, branch and resume checks passed.'
