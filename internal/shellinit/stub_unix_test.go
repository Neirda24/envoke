//go:build !windows

package shellinit

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// installStub writes the stand-in `envoke` spec describes into dir, as a
// `#!/bin/sh` script named `envoke` — the interpreter every shell driven here
// can reach, and the reason the four POSIX drivers cannot run on Windows
// (requirePOSIXHarness).
//
// The three shapes are separate scripts rather than one that branches, so each
// stays the smallest thing that answers the question its test asks.
func installStub(t *testing.T, dir string, spec stubSpec) {
	t.Helper()

	var stub string
	switch {
	case spec.ExitCode != 0:
		stub = fmt.Sprintf("#!/bin/sh\nexit %d\n", spec.ExitCode)
	case spec.Emit != "":
		// A quoted heredoc (`<<'ENVOKE_EOF'`) rather than a plain
		// `echo "..."`, so the line can carry metacharacters meaningful to
		// *other* shells — notably PowerShell's `$env:NAME = 'value'`, whose
		// leading `$` this /bin/sh script would otherwise expand before it
		// ever reached stdout.
		stub = "#!/bin/sh\n" +
			`if [ "$1" = "shell-hook" ]; then cat <<'ENVOKE_EOF'` + "\n" +
			spec.Emit + "\n" +
			"ENVOKE_EOF\n" +
			"fi\n"
	default:
		stub = "#!/bin/sh\n" +
			`if [ "$1" = "shell-hook" ]; then` + "\n" +
			`  shift; if [ "$1" = "--shell" ]; then shift 2; fi; if [ "$1" = "--" ]; then shift; fi` + "\n" +
			`  if [ "$#" -eq 0 ]; then set -- "$ENVOKE_FROM" "$ENVOKE_TO"; fi` + "\n" +
			`  echo "$1 $2" >> ` + shellQuote(spec.LogPath) + "\n" +
			"fi\n"
	}

	if err := os.WriteFile(filepath.Join(dir, "envoke"), []byte(stub), 0o755); err != nil {
		t.Fatalf("WriteFile stub: %v", err)
	}
}
