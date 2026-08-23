//go:build windows

package shellinit

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The stand-in `envoke` a hook driver puts on PATH is a `#!/bin/sh` script
// everywhere else. Windows has no shebang handling, and the two obvious
// substitutes both change what the test asserts:
//
//   - a `.cmd`/`.bat` shim receives its arguments through cmd.exe's own
//     re-parsing, so a driver would be testing that rather than the hook, and
//     `echo` there writes CRLF where the assertions expect one "\n";
//   - a PowerShell function or a `.ps1` named `envoke` is not a native command,
//     and PowerShell only clobbers $LASTEXITCODE for native ones — the leak
//     TestGenerate_HooksAreTransparentToLastCommandStatus exists to catch would
//     stop being observable at all.
//
// So the stub is this very test binary under another name, re-entered through
// TestMain: a real executable, handed its argv by the OS exactly as the real
// `envoke` would be. What it should do with an invocation is read from a JSON
// file written beside it, which keeps the drivers themselves platform-neutral
// — they install a stub and set PATH, as they do everywhere else.
const stubSpecName = "envoke.stub.json"

// stubExeName is the file name PowerShell resolves for the bare word `envoke`,
// via PATHEXT.
const stubExeName = "envoke.exe"

// TestMain exists for the stub and nothing else. Because the switch is the
// binary's own name rather than an environment variable, an ordinary `go test`
// can never take the stub path, and a driver cannot forget to arm it.
func TestMain(m *testing.M) {
	if dir, ok := stubInvocation(); ok {
		os.Exit(runStub(dir, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// stubInvocation reports the directory the running image lives in when it was
// launched as `envoke` rather than as this package's test binary. Both
// os.Args[0] and os.Executable are consulted: the first is whatever the
// launcher passed, the second is the image path Windows resolved, and only the
// second is reliably absolute.
func stubInvocation() (dir string, ok bool) {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	if !isStubName(exe) && !isStubName(os.Args[0]) {
		return "", false
	}
	return filepath.Dir(exe), true
}

func isStubName(path string) bool {
	return path != "" && strings.EqualFold(filepath.Base(path), stubExeName)
}

// runStub is the stub's whole behaviour, mirroring the `#!/bin/sh` scripts
// installStub writes elsewhere — including the real cmdShellHook's argument
// handling, so a hook that stopped passing the directories logs nothing
// usable rather than nothing at all.
func runStub(dir string, args []string) int {
	data, err := os.ReadFile(filepath.Join(dir, stubSpecName))
	if err != nil {
		fmt.Fprintln(os.Stderr, "envoke stub:", err)
		return 97
	}
	var spec stubSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		fmt.Fprintln(os.Stderr, "envoke stub:", err)
		return 97
	}

	if spec.ExitCode != 0 {
		return spec.ExitCode
	}
	if len(args) == 0 || args[0] != "shell-hook" {
		return 0
	}

	rest := args[1:]
	if len(rest) >= 2 && rest[0] == "--shell" {
		rest = rest[2:]
	}
	if len(rest) >= 1 && rest[0] == "--" {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		rest = []string{os.Getenv("ENVOKE_FROM"), os.Getenv("ENVOKE_TO")}
	}
	var from, to string
	if len(rest) > 0 {
		from = rest[0]
	}
	if len(rest) > 1 {
		to = rest[1]
	}

	if spec.Emit != "" {
		fmt.Print(spec.Emit + "\n")
	}
	if spec.LogPath != "" {
		f, err := os.OpenFile(spec.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			fmt.Fprintln(os.Stderr, "envoke stub:", err)
			return 97
		}
		defer func() { _ = f.Close() }()
		if _, err := fmt.Fprintf(f, "%s %s\n", from, to); err != nil {
			fmt.Fprintln(os.Stderr, "envoke stub:", err)
			return 97
		}
	}
	return 0
}

// installStub puts a copy of this test binary in dir under the name the shell
// will resolve, alongside the spec that tells it what to do.
func installStub(t *testing.T, dir string, spec stubSpec) {
	t.Helper()

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal stub spec: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, stubSpecName), data, 0o600); err != nil {
		t.Fatalf("WriteFile stub spec: %v", err)
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("Executable: %v", err)
	}
	stub := filepath.Join(dir, stubExeName)
	// A hard link is instant and costs no disk, which matters because a test
	// binary is tens of megabytes and every driver installs its own stub. It
	// needs both paths on one volume (they are both under the temp directory),
	// so a copy is the fallback rather than the rule.
	if err := os.Link(self, stub); err == nil {
		return
	}
	copyStub(t, self, stub)
}

func copyStub(t *testing.T, from, to string) {
	t.Helper()

	src, err := os.Open(from)
	if err != nil {
		t.Fatalf("open %s: %v", from, err)
	}
	defer func() { _ = src.Close() }()

	dst, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatalf("create %s: %v", to, err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		t.Fatalf("copy stub: %v", err)
	}
	if err := dst.Close(); err != nil {
		t.Fatalf("close stub: %v", err)
	}
}
