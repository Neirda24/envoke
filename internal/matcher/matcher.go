// Package matcher resolves which enter/leave config blocks fire for a
// directory change, including ondir-style traverse behavior: jumping
// straight from /a to /a/x/y/z still fires the rules for the intermediate
// directories /a/x and /a/x/y, not just the destination.
package matcher

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Neirda24/envoke/internal/config"
)

// Match is a single fired rule: which block matched, and the directory it
// matched against (not necessarily from or to — it may be an intermediate
// directory on the path between them).
type Match struct {
	Block config.Block
	// Config gates execution: nothing runs before this particular file's
	// trust is checked, and with several configs in play the answer differs
	// per match.
	Config *config.Config
	// Dir is the directory the shell reported, symlinks unresolved and in the
	// platform's own form: it is used as a working directory and handed to
	// scripts as ENVOKE_DIR.
	Dir string
	// Groups holds the pattern's submatches against the path that actually
	// matched — MatchPath(Dir), or its resolved form for a confined config.
	// Stored so the pattern runs once per candidate directory rather than
	// once to test and again to extract.
	Groups []string
}

// NewMatch runs b's pattern against dir once, refusing the match when cfg is
// a confined config and dir does not lie inside its subtree. Everything that
// builds a Match goes through here, so the confinement rule cannot end up
// implemented two different ways.
func NewMatch(cfg *config.Config, b config.Block, dir string) (Match, bool) {
	return newMatch(cfg, b, &candidate{dir: dir})
}

// newMatch is NewMatch over a candidate whose resolution is shared with the
// other configs and blocks tested against the same directory. Unexported so
// no caller outside the package can supply a candidate it did not compute.
func newMatch(cfg *config.Config, b config.Block, c *candidate) (Match, bool) {
	against := c.dir
	// Not recoverable from `against != c.dir`: a project with no symlink above
	// it resolves to its own spelling, and that case still owes the identity
	// bound.
	boundByIdentity := false
	if cfg.Local {
		// A confined config's Dir and pattern base are physical
		// (config.LoadFragmentResolved); c.dir is the shell's own $PWD, which
		// is not. Where an ancestor is a symlink the two disagree and every
		// block in the fragment stops firing — on macOS, any project under
		// /var.
		//
		// A directory that will not resolve is compared as spelled. Refusing
		// outright would cost the leave blocks of a directory removed
		// underfoot, and nothing else unwinds those.
		if physical, ok := c.resolve(); ok {
			against, boundByIdentity = physical, true
		} else if !Within(c.dir, cfg.Dir) {
			return Match{}, false
		}
	}
	// Pattern before bound: both must hold and neither observes the other, but
	// a confined fragment's ./ patterns carry cfg.Dir as a literal prefix
	// (config.compilePattern), so outside the project the regex refuses with
	// no syscall where withinBound may stat every ancestor up to the root.
	groups := b.Pattern.FindStringSubmatch(MatchPath(against))
	if groups == nil {
		return Match{}, false
	}
	if boundByIdentity && !c.withinBound(cfg.Dir) {
		return Match{}, false
	}
	return Match{Block: b, Config: cfg, Dir: c.dir, Groups: groups}, true
}

// candidate is one directory being tested, holding its symlink-resolved form
// and its answer to each confinement bound once something has asked for them.
// Both cost syscalls per path component and are per directory, not per
// (directory, config, block) — collect tests every block of every config
// against the same candidate.
type candidate struct {
	dir      string
	physical string
	ok       bool
	resolved bool

	// bounds is withinBound's answer per base, written only for a base the
	// lexical comparison rejected. Keyed on base: two confined fragments in
	// one set have different bounds.
	bounds map[string]bool
}

// resolve returns dir with every symlink followed, computed at most once. ok
// is false for a directory that no longer exists, or one with a component the
// kernel will not follow.
func (c *candidate) resolve() (string, bool) {
	if !c.resolved {
		c.resolved = true
		if physical, err := filepath.EvalSymlinks(c.dir); err == nil {
			c.physical, c.ok = physical, true
		}
	}
	return c.physical, c.ok
}

// withinBound reports whether this candidate's resolved form is base or lies
// underneath it: the confinement test for a config bounded to base.
//
// Within is lexical, so two spellings of one directory read as two — which a
// case-insensitive filesystem (macOS by default) makes routine. os.SameFile
// settles it and cannot widen the bound: an ancestor it accepts *is* base, so
// the resolved directory is physically inside it.
//
// That soundness rests on the walk starting from the resolved path, which the
// signature cannot state. Walk c.dir instead and /proj/escape — a link out of
// the project — has base among its ancestors and is admitted.
func (c *candidate) withinBound(base string) bool {
	physical, ok := c.resolve()
	if !ok {
		return false
	}
	if Within(physical, base) {
		return true
	}
	if within, asked := c.bounds[base]; asked {
		return within
	}
	within := sameDirOrAncestor(physical, base)
	if c.bounds == nil {
		c.bounds = make(map[string]bool, 1)
	}
	c.bounds[base] = within
	return within
}

// sameDirOrAncestor reports whether base is dir itself or one of dir's
// ancestors, by file identity rather than by spelling.
func sameDirOrAncestor(dir, base string) bool {
	// Refused on Windows, and the guard is on the primitive rather than the
	// caller so no future path in the package can obtain an identity answer
	// there. os.SameFile compares (volume serial, file index); the file index
	// is unsupported on ReFS, a directory-entry offset on FAT/exFAT, and
	// absent from some SMB redirectors, so two distinct directories can
	// compare equal. withinBound reaches this only for a directory Within has
	// already placed outside base, so a false positive is a confinement
	// bypass, not a missed match.
	//
	// Nothing is lost: EvalSymlinks normalises each component to its on-disk
	// spelling there (8.3 names included) and filepath.Rel folds case, so
	// Within answers the two-spellings question by itself. Deleting this guard
	// reopens the bypass.
	if runtime.GOOS == "windows" {
		return false
	}
	baseInfo, err := os.Stat(base)
	if err != nil {
		return false
	}
	p := dir
	for {
		if info, err := os.Stat(p); err == nil && os.SameFile(info, baseInfo) {
			return true
		}
		parent := filepath.Dir(p)
		if parent == p {
			return false
		}
		p = parent
	}
}

// Within reports whether dir is base or lives underneath it, lexically.
//
// filepath.Rel already understands the platform's path rules: on Windows two
// different volumes have no relative path at all, which is the "not within"
// answer. Touching no filesystem is what keeps it usable on a path that need
// not exist, which configset.confine and newMatch's fallback both depend on —
// see withinBound for the other half of the bound.
func Within(dir, base string) bool {
	rel, err := filepath.Rel(base, dir)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

// MatchPath is the form of a path that patterns are matched against:
// forward-slash separated, whatever the platform uses natively.
//
// Patterns are regexes and `\` is the escape character, so they are written
// with `/`. It must stay filepath.ToSlash and never a blind ReplaceAll: `\`
// is a legal character in a Unix filename.
func MatchPath(dir string) string {
	return filepath.ToSlash(dir)
}

// Resolve computes which leave blocks fire walking out of from, and which
// enter blocks fire walking into to.
//
// from and to must be absolute; they're cleaned internally but not otherwise
// resolved, so what the shell reported is what a pattern is matched against —
// except for a confined config, whose bound and patterns are physical (see
// newMatch).
//
// cfgs must be ordered outermost-first, as configset.Load produces. Enters
// apply in that order and leaves in reverse, so a transition unwinds in the
// order it was applied; leaves are also deepest-directory-first and enters
// shallowest-first.
func Resolve(cfgs []*config.Config, from, to string) (leaves, enters []Match, err error) {
	left, entered, err := Transitions(from, to)
	if err != nil {
		return nil, nil, err
	}

	leaves = collect(reversed(cfgs), left, config.Leave)
	enters = collect(cfgs, entered, config.Enter)
	return leaves, enters, nil
}

// Enters returns every enter block matching dir or any of its ancestors,
// shallowest first: the set that would have fired arriving from outside the
// filesystem entirely.
//
// This is what `envoke reload` needs and Resolve cannot express — Resolve
// answers "what changed", so from == to yields nothing and passing the root as
// from still skips the root itself. Leave blocks have no equivalent: nothing
// has been left, and enter and leave are independent.
func Enters(cfgs []*config.Config, dir string) ([]Match, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("path %q is not absolute", dir)
	}
	return collect(cfgs, ancestors(dir), config.Enter), nil
}

// collect returns every block of the given type matching any of dirs, in
// (directory, config, declaration) order. The candidate is built in the outer
// loop so a directory is resolved at most once however many confined configs
// and blocks are tested against it.
func collect(cfgs []*config.Config, dirs []string, want config.BlockType) []Match {
	var matches []Match
	for _, dir := range dirs {
		c := candidate{dir: dir}
		for _, cfg := range cfgs {
			for _, b := range cfg.Blocks {
				if b.Type != want {
					continue
				}
				if m, ok := newMatch(cfg, b, &c); ok {
					matches = append(matches, m)
				}
			}
		}
	}
	return matches
}

func reversed(cfgs []*config.Config) []*config.Config {
	out := make([]*config.Config, len(cfgs))
	for i, cfg := range cfgs {
		out[len(cfgs)-1-i] = cfg
	}
	return out
}

// Transitions splits a directory change from -> to into the ancestor
// directories left (deepest first) and entered (shallowest first), relative
// to their common ancestor. If from == to, both are empty.
func Transitions(from, to string) (left, entered []string, err error) {
	if !filepath.IsAbs(from) {
		return nil, nil, fmt.Errorf("from path %q is not absolute", from)
	}
	if !filepath.IsAbs(to) {
		return nil, nil, fmt.Errorf("to path %q is not absolute", to)
	}

	fromChain := ancestors(from)
	toChain := ancestors(to)

	common := 0
	for common < len(fromChain) && common < len(toChain) && fromChain[common] == toChain[common] {
		common++
	}

	for i := len(fromChain) - 1; i >= common; i-- {
		left = append(left, fromChain[i])
	}
	entered = append(entered, toChain[common:]...)
	return left, entered, nil
}

// ancestors returns p's ancestor chain, root-first, ending with p itself.
func ancestors(p string) []string {
	p = filepath.Clean(p)
	var chain []string
	for {
		chain = append(chain, p)
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
		p = parent
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}
