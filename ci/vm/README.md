# Veil CI VM assets

This directory holds the OCI image definitions and guest-side helpers used by
the local CI (`make ci`, `make ci-full`, `make ci-pr`). See
`docs/development/ci.md` for the full guide.

- `Containerfile` — multi-stage image chain: `base` → `browser`, `base` → `system`.
- `image.lock` — pinned Ubuntu 24.04 base digest.
- `packages.lock` — apt package lists per image target.
- `entrypoint.sh` / `guest-run.sh` / `prepare-workspace.sh` — in-guest driver:
  extract the repository snapshot onto the native guest filesystem, create a
  fresh git repo for drift checks, run the requested `scripts/ci/<job>.sh` as
  the correct user (ci / root), mirror artifacts back out.
- `systemd/` — proof-of-concept units for the systemd gate (PID 1, D-Bus,
  service lifecycle, socket activation, journal).

Images are built and content-keyed by `scripts/ci/vm-build.sh`
(`make ci-image`). The image key is a sha256 over Containerfile, locks,
versions, entrypoint and systemd files — unchanged inputs reuse cached images.
