// Package configset assembles the set of configs envoke acts on and loads
// each one exactly once.
//
// The set is your central config plus every fragment in the envokerc.d
// directory, and it is deliberately the same set whatever directory you are
// moving between: every file in it lives in a directory you own. envoke does
// not go looking for configs in the trees it walks through — a config that
// travels with a project joins the set only when you symlink it in, which is
// an act you perform once, knowingly, rather than one a `cd` performs for you.
package configset

import (
	"path/filepath"

	"github.com/Neirda24/envoke/internal/config"
	"github.com/Neirda24/envoke/internal/matcher"
	"github.com/Neirda24/envoke/internal/trust"
)

// Entry is one config file in the set, loaded or failed.
//
// The content bytes travel with the parsed config for the same reason
// config.LoadFile hands them back: whoever checks trust must hash the bytes
// that were parsed, not re-read the file and hash whatever is there by then.
type Entry struct {
	// Path is the config file as envoke reached it, and is what `envoke allow`
	// names and what the trust record is keyed on.
	//
	// For a fragment that is a symlink to a file inside a project, this is the
	// link rather than its target: the link is what the user created and can
	// remove. The enclosing directory, by contrast, is resolved before it is
	// walked (see config.Fragments), so when envokerc.d is itself a symlink
	// into a dotfiles repository these paths name the real files there.
	Path string

	// Fragment distinguishes an envokerc.d file from the central config,
	// available even when the load failed and Config is nil.
	Fragment bool

	// Config and Content are nil when Err is set.
	Config  *config.Config
	Content []byte

	// Err is this file's own read or parse failure. It is per-entry rather
	// than returned wholesale because one unparseable fragment must not stop
	// every other config from working — with a symlinked fragment, the file
	// that breaks may have been rewritten by someone else's commit.
	Err error
}

// Load returns the whole config set: the central config first (when
// globalPath is non-empty), then every fragment under fragmentDir in the
// order config.Fragments defines.
//
// The central config comes first because it is the outermost: matcher.Resolve
// applies the set in order on the way in and in reverse on the way out.
//
// fragmentDir may be "" or may not exist; that yields no fragments and no
// error.
func Load(globalPath, fragmentDir string) []Entry {
	var entries []Entry
	seen := make(map[string]bool)

	if globalPath != "" {
		cfg, content, err := config.LoadFile(globalPath)
		entries = append(entries, Entry{Path: globalPath, Config: cfg, Content: content, Err: err})
		if _, key := identify(globalPath); key != "" {
			seen[key] = true
		}
	}

	if fragmentDir == "" {
		return entries
	}

	// The root the walk resolved is the root the confinement decision needs: a
	// config directory symlinked into a dotfiles repository — the normal
	// dotfiles layout — must compare equal to the resolved fragments inside it,
	// or every fragment looks like it points out of the directory and every one
	// gets confined.
	root, paths, err := config.Fragments(fragmentDir)
	if err != nil {
		return append(entries, Entry{Path: fragmentDir, Fragment: true, Err: err})
	}

	for _, path := range paths {
		resolved, key := identify(path)
		if key != "" {
			if seen[key] {
				// The same file reachable both as the central config and as a
				// fragment — a symlink pointing back at ~/.envokerc, say.
				// Loading it twice would fire every one of its blocks twice
				// per cd.
				continue
			}
			seen[key] = true
		}

		cfg, content, err := config.LoadFragmentResolved(path, resolved)
		entry := Entry{Path: path, Fragment: true, Config: cfg, Content: content, Err: err}
		if cfg != nil {
			cfg.Local = confine(cfg, root)
			entry.Config = cfg
		}
		entries = append(entries, entry)
	}
	return entries
}

// confine reports whether a fragment's blocks must be bounded to its own
// directory — whether, in other words, the file only points into the config
// directory rather than living in it.
//
// Decided here rather than in config.LoadFragmentResolved because it is a
// property of the *set*: only the assembler knows what the root is.
//
// A fragment whose symlink could not be followed is confined whatever its
// directory looks like. Such a file reports the link's own directory as Dir,
// which is inside the root, so it would otherwise read as "a file that really
// lives here" — the one shape that is *not* confined. That is failing open on
// a check whose whole job is to bound what someone else's commit can reach.
// It is not free — a fragment caught here stops firing rather than firing too
// widely — but that is the right way round for a security bound, and the case
// is narrow: a link the kernel reads through and EvalSymlinks won't follow.
func confine(cfg *config.Config, root string) bool {
	return cfg.DirUnresolved || !matcher.Within(cfg.Dir, root)
}

// Configs returns the successfully-loaded configs, in set order, ready to
// hand to matcher.Resolve. Entries that failed to load are dropped: their Err
// is the caller's to report, and matching against a config that isn't there
// is not something matcher should have to express.
func Configs(entries []Entry) []*config.Config {
	cfgs := make([]*config.Config, 0, len(entries))
	for _, e := range entries {
		if e.Err == nil && e.Config != nil {
			cfgs = append(cfgs, e.Config)
		}
	}
	return cfgs
}

// ByConfig indexes the loaded entries by the config they produced, which is
// how a matcher.Match gets back to the file it came from. The key is the
// pointer rather than the path: it is exact, and it does not care that the
// central config's path is whatever the user typed while a fragment's is
// always rooted at the config directory.
func ByConfig(entries []Entry) map[*config.Config]Entry {
	byCfg := make(map[*config.Config]Entry, len(entries))
	for _, e := range entries {
		if e.Config != nil {
			byCfg[e.Config] = e
		}
	}
	return byCfg
}

// Decision is what envoke may do with one config in the set.
type Decision int

const (
	// Run: approved, and the content still matches what was approved.
	Run Decision = iota
	// Untrusted: never approved, or edited since it was.
	Untrusted
	// Failed: the file could not be read or parsed (see Entry.Err).
	Failed
)

// Decide reports what may be done with e. Every command that can execute a
// block goes through here, so the trust rule is stated once.
func Decide(e Entry) (Decision, error) {
	if e.Err != nil {
		return Failed, nil
	}
	trusted, err := trust.IsTrusted(e.Path, e.Content)
	if err != nil {
		return Failed, err
	}
	if trusted {
		return Run, nil
	}
	return Untrusted, nil
}

// identify follows path's symlinks once, for both things that need them.
//
// key identifies the file for the dedup, so that the same file reached two ways
// — the central config, and a fragment that links to it — counts once. Without
// that it would be loaded twice and every one of its blocks would fire twice
// per cd. It is "" when the path can't be resolved at all, which only makes the
// dedup miss, never makes it wrong.
//
// resolved is what config.LoadFragmentResolved needs: EvalSymlinks' answer, or
// "" when it refused to follow the link, which is a different thing from the
// absolute path key falls back to. Resolving is an lstat/readlink loop per path
// component and this runs for every fragment on every cd, so the two callers
// share the one resolution rather than each doing their own.
func identify(path string) (resolved, key string) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, resolved
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", ""
	}
	return "", abs
}
