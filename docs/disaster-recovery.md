# Disaster Recovery And Key Lifecycle

Veil disaster recovery protects the Management state, normalized SQLite
domain, and the key required to decrypt credentials. Treat every archive as a
root credential for the host.

## Protected Material

A complete recovery set contains:

- `/var/lib/veil/state.json`: Management state, users, Inbounds, routing, and
  encrypted secrets.
- `/etc/veil/state.key`: the 32-byte AES-256-GCM state encryption key.
- `/var/lib/veil/veil.db`: normalized clients/bindings, encrypted credentials,
  subscription tokens, traffic, revisions/apply jobs, snapshots, and durable
  legacy-migration provenance.

Archive format v2 contains all three files plus a manifest with the creation
time, Veil version, Management schema version, captured desired revision, file
sizes, and SHA-256 checksums. Creation holds the Management snapshot barrier
across `state.json` capture and SQLite `VACUUM INTO`. Verification checks that
the archived database contains the immutable snapshot for the manifest's
non-zero desired revision. The manifest is inside the same encrypted envelope
when archive encryption is enabled.

New encrypted backups stream tar/gzip data through independently authenticated
1 MiB encryption frames. Creation, verification, download, and restore do not
load `veil.db` or the complete archive into RAM. Legacy encryption versions
remain readable. `VEIL_BACKUP_MAX_BYTES` is an explicit positive byte-count
policy applied to both encrypted archive size and expanded member total; its
production default is 17179869184 bytes (16 GiB). Native services read it from
`/etc/veil/veil.env`; standalone CLI invocations may export it directly.

Before privileged verify or restore starts, Veil checks free space for the
worst-case configured expansion, staging copies, current-file safety copies,
and a reserve. Requirements on the same filesystem are summed. Restore-job
history is persisted; a queued or running job found after process restart is
marked failed rather than disappearing. Before a new restore, only the newest
two prior safety copies per state/key/database target are retained so the new
copy becomes the third; older regular files are overwritten, synced, removed,
and their parent directory synced. Symlinks and multi-link files are rejected.

Losing `state.key` makes `state.json` unrecoverable. Store encrypted archives
off-host and keep the archive passphrase outside the backup destination.

## Create And Verify

CLI backup creation is fail-closed: encryption is required unless
`--allow-unencrypted` is supplied explicitly.

```bash
sudo veil backup create \
  --passphrase-file /root/veil-backup-passphrase \
  --output-dir /var/lib/veil/backups \
  --prune \
  --daily 7 \
  --weekly 4 \
  --monthly 12
```

Avoid `--passphrase` on shared systems because shell history and process
inspection can expose it. Verify an archive before copying or restoring it:

```bash
sudo veil backup verify \
  /var/lib/veil/backups/veil_backup_20260605_020000.tar.gz.enc \
  --passphrase-file /root/veil-backup-passphrase
```

List managed archives and preview retention:

```bash
sudo veil backup list --dir /var/lib/veil/backups
sudo veil backup prune --dir /var/lib/veil/backups \
  --daily 7 --weekly 4 --monthly 12 --dry-run
```

## Scheduled Backups

Native packages ship `veil-backup.service` and `veil-backup.timer`. Enable the
daily encrypted schedule with a passphrase of at least 16 characters:

```bash
sudo veil backup schedule enable \
  --passphrase-file /root/veil-backup-passphrase
systemctl list-timers veil-backup.timer
```

The command installs the root-owned passphrase at
`/etc/veil/backup.passphrase` with mode `0600`. The timer writes verified
archives to `/var/lib/veil/backups` and applies the default 7 daily, 4 weekly,
and 12 monthly retention policy.

The Panel backup screen uses the same server-side passphrase. The browser never
receives it. Disable the timer and optionally remove the stored passphrase:

```bash
sudo veil backup schedule disable --remove-passphrase
```

Copy retained archives to an independent host or object store. A timer on the
same machine protects against operator error, not total host loss.

## Restore Check

Run a no-write compatibility check first:

```bash
sudo veil backup restore \
  /path/to/veil_backup_20260605_020000.tar.gz.enc \
  --passphrase-file /root/veil-backup-passphrase \
  --check-only
```

The check decrypts the archive, validates checksums, validates the state/key
pair and SQLite integrity/revision snapshot, and rejects a state schema newer
than the running Veil release. You can
also structurally validate any Management state file (for example a restored
`state.json` before starting the Panel) without touching the live server:

```bash
veil config validate --state /path/to/state.json
```

## Restore On The Current Host

Stop the Panel so no process writes state during recovery:

```bash
sudo systemctl stop veil.service
sudo veil backup restore \
  /path/to/veil_backup_20260605_020000.tar.gz.enc \
  --passphrase-file /root/veil-backup-passphrase \
  --state /var/lib/veil/state.json \
  --key-path /etc/veil/state.key \
  --yes
sudo veil repair --yes
sudo systemctl start veil.service
veil status
```

Before replacement, Veil stages and verifies all archive members, then preserves
existing state, key, and database files as timestamped `.pre-restore-*` safety
copies. Any commit failure rolls all three back. Keep the copies until the
restored Panel and all managed protocols have been validated.

Panel restores are queued, admin-only jobs. The Panel stops SQLite collectors,
closes the database before replacement, then reloads state/key and reopens every
SQLite-backed store before reporting success. A successful restore revokes all
browser sessions, including the initiating session after it receives the final
job result.

## Restore On A New Host

1. Install the same or a newer Veil release.
2. Transfer the encrypted archive and passphrase through separate channels.
3. Run `veil backup restore --check-only`.
4. Stop `veil.service` and perform the restore command shown above.
5. Validate the restored state structurally (`veil config validate`), then run
   `veil repair --yes` to regenerate managed files and units for the new
   host.
6. Start Veil, inspect `veil status`, and test every enabled Inbound.
7. Rotate the state key if the old host or its key may be compromised.
8. Create and export a fresh backup from the new host.

Do not copy generated runtime files instead of using `veil repair`; host paths,
service availability, firewall state, and installed runtime versions may
differ.

## State Key Rotation

Rotate the key in place:

```bash
sudo veil admin rotate-key
```

Or write the replacement key to another path:

```bash
sudo veil admin rotate-key \
  --new-key-path /etc/veil/rotated-state.key
```

Rotation re-encrypts Management state atomically and rolls back failed file
replacement. Existing archives remain self-contained because each archive
includes the state key used by its state file. Create a fresh encrypted backup
after every rotation.

## Compatibility Policy

Veil restores:

- legacy encrypted envelope v1;
- authenticated encrypted envelope v2;
- legacy two-file archive/manifest format v1 (leaves the existing `veil.db`
  untouched);
- current three-file archive/manifest format v2;
- older supported Management state schemas.

Committed v1 and v0.5.0-v2 archive fixtures are restored by the test suite on
every CI run. Archives with a newer Management schema are rejected before any
write.

## Recovery Drill

At least quarterly:

1. Copy a recent archive to an isolated host.
2. Verify it and run `restore --check-only`.
3. Restore to temporary state/key paths.
4. Start a disposable Veil instance on loopback.
5. Confirm users, Inbounds, routing, exports, and apply preview.
6. Record the archive date, Veil versions, duration, and remediation found.

An untested backup is only a hopeful file.
