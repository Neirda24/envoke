# internal/fsperm

`Unsafe(path)` — the one place that knows what "writable by someone else"
means per platform.

- Exists because `mode&0o022 != 0` was inlined in three packages and is
  meaningless on Windows. The reason is in `fsperm_windows.go`; don't restate
  it at a call site.
- **Never inline the bit test again.** Three copies were three chances to
  forget the platform rule.
- `fsperm_test.go` asserts a *different* answer per platform and runs on the
  Windows CI runner. That is what keeps the inlining from coming back.
