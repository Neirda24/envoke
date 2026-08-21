# internal/fsperm

`Unsafe(path)`, and the one place that knows what "writable by someone else"
means per platform.

It exists because `mode&0o022 != 0` was inlined in three packages, and that
expression is meaningless on Windows: Go synthesises the whole permission word
from the read-only attribute, so `os.Stat` reports `0666` for every writable
file and `0777` for every directory. Every config, and the trust store itself,
read as world-writable there — the store check being on the `cd` path, that was
a warning per prompt.

The Windows build reports nothing rather than something false; reading the real
ACL needs `golang.org/x/sys`, which is a large price for a warning.

`fsperm_test.go` asserts the *different* answer per platform and runs on the
Windows CI runner, which is what keeps this from coming back.
