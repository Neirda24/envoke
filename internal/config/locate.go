package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Locate resolves the path to the user's envoke config, in order:
//
//  1. $ENVOKERC, if set — used verbatim, even if the file doesn't exist yet
//     (an explicit override means "use this path", not "use this path if
//     convenient").
//  2. ~/.envokerc, if it exists (the documented default).
//  3. $XDG_CONFIG_HOME/envoke/config (or ~/.config/envoke/config if
//     XDG_CONFIG_HOME isn't set), if it exists.
//
// found is false when none of the above exist and $ENVOKERC wasn't set —
// that's a normal "nothing to load" state, not an error. path is still set
// in that case (the ~/.envokerc default) so callers can use it in messages.
func Locate() (path string, found bool, err error) {
	if p := os.Getenv("ENVOKERC"); p != "" {
		return p, true, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, fmt.Errorf("locate config: %w", err)
	}

	def := filepath.Join(home, ".envokerc")
	if fileExists(def) {
		return def, true, nil
	}

	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	xdgPath := filepath.Join(xdg, "envoke", "config")
	if fileExists(xdgPath) {
		return xdgPath, true, nil
	}

	return def, false, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
