# internal/trust

Content-hash trust records under `<data home>/envoke/allow/`. Data home comes
from [`internal/state`](../state/CLAUDE.md).

- **`IsTrusted` and `Allow` take content bytes, never a path to re-read.**
  That is what makes the TOCTOU unexpressible (see
  [`internal/config`](../config/CLAUDE.md)'s `LoadFile`). A path-only
  convenience wrapper reopens the hole it was removed to close — don't add
  one.
- A record is three files: `<hash>` (the token), `.content` (for the diff),
  `.path` (the absolute path — the record name is a one-way hash, so nothing
  else could answer "what have I trusted?"). Both siblings are optional on
  read, so upgrading never revokes anyone's trust.
- The record name's format is stable. Changing it revokes every existing
  approval.
- `cmdAllow` combines `PreviousContent` **and** `IsTrusted` to pick full-dump
  vs diff vs "nothing changed". Content equality alone wedges on a torn
  record.
- `Prune` leaves path-less legacy records alone rather than guessing whether
  their config still exists.
