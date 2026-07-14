# Debugging

`envoke debug <from> <to>` prints which `enter`/`leave` blocks would fire for a given directory transition, without ever running them.

```sh
envoke debug ~/Projects ~/Projects/envoke
```

This runs the same resolution logic (`matcher.Resolve`) as the live `shell-hook`, and additionally reports whether the config is currently trusted — but it never calls the code path that executes or renders a script, regardless of trust status. That's the point: `envoke debug` is safe to run against a config you haven't approved yet, or one you're actively editing and don't want to accidentally trigger.

Use it to:

- Develop a new config without surprises — see exactly which blocks a transition would match before you `envoke allow` it.
- Confirm a pattern change matches (or stops matching) the directories you expect.
- Check trust status without needing to inspect the trust store directly.
