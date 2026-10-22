# Go toolchain security notice (2026-10-22)

`paylayer` has no `plan/` directory, so this is a plain note rather
than a formatted cross-repo message like the ones in `cinder`'s,
`offgrid`'s, and `wisp`'s `plan/inbox/`.

While checking a user-reported Go CVE, EphemNet found its own
`go.mod`'s `go 1.24.0` had been unpatched since 2026-02-11 — the day
Go 1.26 shipped and Go stopped backporting security fixes to 1.24 (Go
only patches the latest two major branches). `paylayer`'s own
`go.mod` is pinned to the same `go 1.24.0`.

As a library, this matters less directly — consumers compile
`paylayer` with their own toolchain, so this repo's `go.mod` line
mainly sets the *minimum* Go version a consumer needs, not which
toolchain actually builds the binary. Still worth bumping for the
same reason EphemNet did: `go1.26.9` (released 2026-10-08) is the
current patch release and picks up real fixes, including
`CVE-2026-94447` (toolchain checksum-bypass).

EphemNet's own fix is written up in full at
`../EphemNet/plan/actions/A022-go-toolchain-security-bump-1-26.md`.
Not changed here — just flagging it.
