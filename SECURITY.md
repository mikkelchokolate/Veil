# Security Policy

Veil is security-sensitive infrastructure software. Please do not open public issues for vulnerabilities.

Until a dedicated address is published, report vulnerabilities privately to the repository owner.

Security principles:
- generated configs are validated before apply
- configs are backed up before replacement
- service restarts must be followed by health checks
- admin passwords must be hashed, never stored in plaintext
- installer must verify downloaded release checksums
