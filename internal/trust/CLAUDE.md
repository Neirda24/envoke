# internal/trust

Trust is a SHA-256 hash of the config file's *content*, recorded under
`$XDG_DATA_HOME/envoke/allow/<sha256(abs path)>` (or `~/.local/share/...`) —
one record per config path. Any edit changes the hash and revokes trust until
`Allow` runs again. The data-home resolution is `internal/state`'s
(see [its notes](../state/CLAUDE.md) for why state lives there, not under
`$XDG_CONFIG_HOME`).

**Both take the content bytes, never a path to re-read** — that's what makes
the TOCTOU unexpressible (see [`internal/config`](../config/CLAUDE.md)'s
`LoadFile`); don't add a path-only convenience wrapper, it would reopen the
hole it was removed to close.

`Allow` also persists the approved content to a sibling `<record>.content`
file, written *before* the hash record so a torn write fails closed, and read
back by `PreviousContent` so `cmdAllow` can show a diff instead of a full
re-dump on re-approval — a pre-existing hash-only record (no content file) is
a normal state (`ok=false`), not corruption.

`shell-hook` calls `IsTrusted` before ever calling `executor.Render`;
`cmdAllow` combines `PreviousContent` *and* `IsTrusted` to decide full-dump
vs. diff vs. "nothing changed" (content equality alone would wedge on a torn
record).

A record is three sibling files — `<hash>` (the trust token), `.content` (for
the diff) and `.path` (the config's absolute path, since the record name is a
one-way hash and nothing else could answer "what have I trusted?"). Both
siblings are optional on read so upgrading never revokes anyone's trust; the
hash file is written **last** and every file is written atomically, so a torn
write fails closed.

`List`/`Revoke`/`Prune` back `envoke list`/`revoke`/`prune`; `Prune`
deliberately leaves path-less legacy records alone rather than guessing
whether their config still exists.

`UnsafeStorePermissions` walks the store **and its ancestors up to the data
home** (`storeChain`), naming whichever is writable: a `0700` store inside a
`0777` parent is a `0777` store, since whoever can write the parent renames
the store away and puts their own there. It stops at the data home on purpose
— above that, a writable directory means the whole home is, which is not a
fact about envoke.
