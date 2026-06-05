# Backup compatibility fixtures

These encrypted archives are immutable release-compatibility inputs:

- `legacy-v1.enc` covers the original PBKDF2/AES-GCM envelope.
- `v0.5.0-v2.enc` covers the authenticated v2 envelope before archive
  manifests were introduced.

The test-only passphrase is `veil-compatibility-fixture`. Regenerate the files
only when intentionally replacing the fixture corpus:

```bash
VEIL_UPDATE_BACKUP_FIXTURES=1 go test ./internal/backup \
  -run TestCommittedBackupCompatibilityFixtures
```
