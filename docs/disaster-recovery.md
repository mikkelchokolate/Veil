# Disaster Recovery and Key Lifecycle Guide

This guide details the procedures and best practices for backing up, restoring, and managing the encryption key lifecycle of a Veil panel installation.

---

## 1. Critical Components and Files

A complete Veil panel configuration and state are stored in exactly two files. To successfully back up, restore, or migrate Veil, both files must be preserved:

1. **State File (`/var/lib/veil/state.json`)**: Contains all configured inbounds, clients, routing rules, users, and masquerading rules. This file is encrypted at rest using AES-256-GCM.
2. **State Key (`/etc/veil/state.key`)**: A 32-byte (256-bit) cryptographically secure random key used to decrypt the state file.

> [!CAUTION]
> If you lose the State Key (`state.key`), you will NOT be able to read or restore the State File (`state.json`). Keep the key secure and separated from the state files in your offline archives.

---

## 2. State Backups

Veil provides built-in commands to package and encrypt your configuration state.

### Creating a Backup

To create a backup, run:
```bash
veil backup create
```
By default, this will package `/var/lib/veil/state.json` and `/etc/veil/state.key` into a gzipped tarball in your current directory, named `veil_backup_YYYYMMDD_HHMMSS.tar.gz`.

#### Passphrase-Based Encryption (Recommended)
You should encrypt backups containing private keys and configuration details using a passphrase:
```bash
veil backup create --passphrase "your-secure-passphrase" --output /path/to/backup.enc
```
Or read the passphrase from a file:
```bash
veil backup create --passphrase-file /etc/veil/backup_pass.txt -o /path/to/backup.enc
```
When a passphrase is provided, the backup is encrypted using PBKDF2 (10,000 iterations of SHA-256) and AES-256-GCM.

### Automating Backups with Cron

To schedule a daily encrypted backup, add the following cron job (e.g. via `crontab -e` as `root`):
```cron
0 2 * * * /usr/local/bin/veil backup create --passphrase-file /etc/veil/backup_pass.txt -o /var/backups/veil/veil_backup_$(date +\%F).enc
```

---

## 3. State Restoration

To restore your state from a backup (unencrypted or encrypted), use the `veil backup restore` command.

> [!WARNING]
> Restoring a backup will overwrite any existing `/var/lib/veil/state.json` and `/etc/veil/state.key` files.

### Restoring an Encrypted Backup
```bash
veil backup restore /path/to/backup.enc --passphrase "your-secure-passphrase"
```
Or using a passphrase file:
```bash
veil backup restore /path/to/backup.enc --passphrase-file /etc/veil/backup_pass.txt
```

### Restoring an Unencrypted Backup
```bash
veil backup restore /path/to/backup.tar.gz
```

> [!NOTE]
> By default, the `restore` command will prompt you for confirmation if run interactively. If you are scripting the restoration, add the `-y` or `--yes` flag to bypass this confirmation.

---

## 4. Key Rotation

Rotating the state key regularly limits the impact of key exposure and updates the encryption layer on the state database file.

### In-Place Key Rotation
To rotate the key file in-place (regenerating the key, decrypting the current state file, re-encrypting it with the new key, and updating the key and state files atomically):
```bash
veil admin rotate-key
```

### Rotating Key to a New Destination
To rotate the key and save it to a new location (for example, if you are migrating paths or externalizing the key store):
```bash
veil admin rotate-key --new-key-path /etc/veil/rotated_state.key
```

### Key Rotation Safeguards
The key rotation mechanism is atomic:
- It creates temporary files (`state.json.tmp` and `state.key.tmp`) to ensure all writes complete successfully.
- It renames files to target paths sequentially.
- If writing the rotated files or renaming the state file fails, it automatically rolls back the key file to the original bytes to prevent locking you out of the system.
