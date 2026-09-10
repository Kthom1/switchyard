# Back up and restore Switchyard

[← Switchyard](../README.md) · [CLI setup](cli.md)

Configuration and task workspaces live in `~/.switchyard`, or your selected
`SWITCHYARD_HOME`. Plane's database and attachments live in Docker named volumes.
Copying the installation directory alone does not back up the board. Keep the
installation path stable: the CLI derives its Docker project and service names
from that absolute path.

A backup contains a Postgres dump, the complete MinIO uploads volume, private
`.env.plane`, `.env`, `WORKFLOW.md`, and CLI `config.json`. The local board's
`plane.json` login credentials are included when present. Legacy dedicated
runner files under `work/codex` are retained when present; your normal shared
Codex home is not part of the board backup. Each snapshot has checksums,
an image list and a source version label. Retain the matching release bundle
for recovery. Snapshots contain credentials and private task data; local copies
are permission-protected, not encrypted.

`WORKFLOW.md` includes every connected project-to-repository mapping. One
installation backup covers all projects on its Plane board.

Backups cover configured installations using the bundled Postgres and MinIO.
They do not include task workspaces, unpushed branches, logs, downloaded plugins
or external storage. Push or separately retain workspaces with unfinished work.
Keep Git hosting and backup recovery credentials independently of the machine.
Set up Codex separately on a replacement machine, including its login, plugins
and configuration.

## Back up a CLI installation

```bash
switchyard backup /path/to/private-backups
switchyard up
```

`backup` stops the runner and Plane writers, starts only Postgres, waits for it
to be ready, then creates one consistent snapshot. It prints the completed
snapshot directory. Without a directory argument, snapshots go in
`work/backups` inside the installation. Copy completed snapshots to another
machine or use the optional encrypted storage below to survive loss of the host.

The command leaves only Postgres running. If backup fails, no incomplete local
snapshot is published; previous backups remain intact. Inspect the error, then
run `switchyard up` when you want to resume service. An off-machine upload failure
retains the completed local snapshot.

Use the same `SWITCHYARD_HOME` as your daily commands. Backup uses that
installation's service and Docker project automatically.

## Restore a CLI installation

Use the matching release bundle and installed prerequisites. Choose a new,
empty installation directory. Do not run `init` there before restoring, because
recovery must use the original database and encryption secrets.

```bash
export SWITCHYARD_HOME="$HOME/.switchyard-recovered"
switchyard restore /path/to/private-backups/switchyard-TIMESTAMP-SUFFIX
```

A separately built CLI can locate its runtime with
`switchyard restore --runner /path/to/bundle/bin/symphony BACKUP_DIRECTORY`.
Keep the full bundle together, including its notices.

Only restore backups you trust: their shell and workflow files are executable
operator configuration. Restore verifies checksums and refuses a nonempty
installation, an existing runner service, or an existing Docker project/data
volume. A failure leaves the new target for diagnosis; retry into another fresh
destination after fixing the cause. It never replaces an existing installation.

The restored database and uploads get the new installation's Docker project
name. The default task workspace location is updated to its `workspaces`
directory. Custom paths in `WORKFLOW.md` and Codex configuration are preserved
and need review. Task checkout contents are not restored by this command.

Restore starts only Postgres. Before starting the board, review:

- `config.json`: board port, runner port and browser origin.
- `.env.plane`: `APP_DOMAIN`, `WEB_URL` and `CORS_ALLOWED_ORIGINS`.
- `WORKFLOW.md`: `tracker.provider.endpoint`, `tracker.provider.web_url`,
  `tracker.provider.projects`, `server.port`, custom hooks and any absolute paths.
- The snapshot's `images.txt` and the matching release's image configuration.
  Recover first; upgrade images separately.

On a replacement machine, keep the original ports and origin when possible.
For a same-machine drill, select unused board/runner ports and update the files
above consistently before starting the board. The tracker endpoint is the new
local board URL; its web URL is the browser origin.

Start only the board for inspection:

```bash
switchyard init --clean
switchyard project list
```

This reuses the restored configuration and does not start the runner. The
restored `plane.json`, when present, retains your original board login. Sign in
and verify representative tasks, comments and a downloaded attachment. Review
the repository mappings and which tasks are eligible to run in each project.
When ready to resume agent work:

```bash
codex login status                  # Check Codex on this machine.
# If needed: codex login --device-auth
switchyard up
```

Keep `SWITCHYARD_HOME` set to this recovered installation for future commands.
The recovery drill should verify your own Plane data, account access and any
custom workflow paths before you rely on a backup.

## Optional encrypted off-machine storage with restic

Install restic using its supported package or release. Initialize a repository
once, then configure its standard environment in a subshell:

```bash
(
export RESTIC_REPOSITORY=/path/to/restic-repository
export RESTIC_PASSWORD_FILE=/path/to/private/restic-password
restic init
switchyard backup
restic check
)
switchyard up
```

When `RESTIC_REPOSITORY` or `RESTIC_REPOSITORY_FILE` is set, the script asks restic
to back up the completed local snapshot with the `switchyard` tag. An upload
failure returns a failure status and retains the complete local snapshot.
Restic handles encryption, deduplication, integrity, and storage.

For S3-compatible storage, set `RESTIC_REPOSITORY` to
`s3:https://YOUR_ENDPOINT/YOUR_BUCKET` and export the provider's
`AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`. Use dedicated backup credentials,
not the local MinIO credentials. Export the S3 configuration inside a subshell
around `switchyard backup`, then leave that subshell before restarting Plane. This
prevents the backup provider's `AWS_*` variables from overriding Plane's own
MinIO credentials through Compose environment precedence. Keep both repository access and the restic
password in an independent password manager or recovery location. Losing the
password makes the encrypted backup unrecoverable. Do not store your only copy
inside the backup it unlocks.

Follow [restic repository setup](https://restic.readthedocs.io/en/stable/030_preparing_a_new_repo.html)
for backend details and [restic restore](https://restic.readthedocs.io/en/stable/050_restore.html)
for retrieval. For example, `restic snapshots --tag switchyard`, then
`restic restore SNAPSHOT_ID --target /path/to/private-recovery`. Locate the restored
`switchyard-TIMESTAMP-SUFFIX` directory and pass it to `switchyard restore` (or `scripts/restore` for a legacy installation). Test
recovery before configuring a standard restic `forget`/`prune` retention policy.
No automatic backup deletion or scheduling is configured by these scripts.

## Legacy source installations

For earlier installations managed from a source checkout, use the scripts
below instead of the CLI commands. Stop the runner and Plane writers for the snapshot. Only Postgres may be running;
the backup script refuses otherwise. Adapt the user-service name if yours differs.
These commands intentionally interrupt Plane until the final start commands.

```bash
systemctl --user stop switchyard
scripts/plane stop
scripts/plane up -d plane-db
scripts/backup
scripts/plane up -d
systemctl --user start switchyard
```

If backup fails, inspect the error, then use the final two commands to resume
service. The script never stops or restarts services itself. Backup waits for the final Postgres server to be ready.

Choose another backup parent with `scripts/backup /path/to/private-backups`.
`scripts/plane`, backup, and restore accept `SWITCHYARD_COMPOSE_PROJECT` (default
`switchyard`) and `SWITCHYARD_PLANE_ENV` (default `.env.plane`). Backup normalizes
selected configuration filenames inside its `config/` directory.
For an additional legacy runner, select its private files with
`SWITCHYARD_RUNNER_ENV` and `SWITCHYARD_WORKFLOW`. Set `SWITCHYARD_CODEX_HOME`
when it uses a custom Codex home. Back up each runner configuration separately.

### Restore a legacy source installation

Use a fresh checkout of the same Switchyard revision and the recorded image set.
`source.txt` records the CLI version, Git label or `source-archive`; retain the matching release
source separately. A `-dirty` label means the original uncommitted code must also
be retained separately.
Only restore backups you trust: the environment/workflow files are executable
operator configuration. Compare `images.txt` with `scripts/plane config --images`
after configuration is restored; do not combine recovery with an image upgrade.

Restore refuses existing configuration, runner auth/config, containers, or target
data volumes. It also refuses the default `switchyard` project to reduce the
chance of restoring into a live installation. Choose a new project name and keep
that name for every subsequent Compose command. Do not initialize the target first; restore must preserve the backed-up encryption and service secrets.

```bash
export SWITCHYARD_COMPOSE_PROJECT=switchyard-recovered
scripts/restore /path/to/switchyard-TIMESTAMP-SUFFIX
scripts/plane exec -T plane-db psql -h /var/run/postgresql -U plane -d plane -c '\dt'
```

The script validates checksums, restores Postgres and uploads, and leaves only
Postgres running. A failed restore leaves the new isolated project for diagnosis;
it never removes a prior installation. Inspect the error and retry using another
fresh directory/project, or explicitly remove only the failed project's data
once you have confirmed its identity.

Review the restored host URL, repository/workspace paths, and runner tool settings.
On the replacement machine, keep the same origin. For a separate recovery drill,
change the origin and use an unused loopback port in the local Compose override
before starting Plane; the default proxy binds port 8090. Keep the Symphony
service stopped throughout the drill.

```bash
scripts/plane up -d
```

Sign in to Plane and verify representative tasks, their comments, and an uploaded
attachment. Verify that required private configuration is present. Only start the
runner after confirming the queue and workspace settings are appropriate for the
recovered machine. Keep the selected Compose project in your operator environment
and configuration files private.

## Runnable checks

```bash
python3 test/backup_test.py
python3 test/backup_test.py --docker
go build -o work/switchyard .
python3 test/backup_test.py --docker --cli work/switchyard
# Also round-trip a local encrypted restic repository when restic is installed:
python3 test/backup_test.py --docker --restic
```

The default check injects a dump failure, verifies the previous backup survives,
and verifies backup refuses running writers. Docker mode creates disposable
projects with synthetic database rows representing a task/comment and a synthetic
attachment file, then proves dump/restore and byte equality. It checks that a
second restore cannot overwrite recovered data. These checks use synthetic data
in disposable local projects. Use the recovery drill above to test your own Plane
installation, including sign-in, tasks, comments and attachments. Test cleanup
removes only the disposable projects and their volumes.
