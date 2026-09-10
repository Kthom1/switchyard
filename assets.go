package main

import "embed"

//go:embed deploy/docker-compose.yml deploy/compose.override.yml deploy/Caddyfile deploy/switchyard.service
//go:embed scripts/plane scripts/run scripts/codex-runner scripts/task-workspace scripts/backup scripts/restore scripts/bootstrap-plane.py
//go:embed WORKFLOW.example.md docs/recommended.md docs/backup.md LICENSE
var assets embed.FS
