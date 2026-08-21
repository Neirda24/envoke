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
	// Config is the config the block was declared in. Every consumer needs
	// it: nothing may run before that particular file's trust is checked,
	// and with several configs in play the answer differs per match.
	Config *config.Config
	// Dir is the directory in the platform's own form (backslashes on
	// Windows), because it is used as a working directory and handed to
	// scripts as ENVOKE_DIR. It is the directory the shell reported, symlinks
	// unresolved: that is where the cd landed and what the user is looking
	// at, whichever form the pattern was matched against.
	Dir string
	// Groups holds the pattern's submatches, Groups[0] being the whole
	// match, captured against the slash-normalized form (see MatchPath) of
	// the path the pattern ran against — Dir, or for a confined config Dir's
	// resolved form, since that config's patterns are compiled against
	// physical paths and captures have to come from the path that actually
	// matched. Storing them keeps the pattern running once per candidate
	// directory rather than once to test and again to extract — this is the
	// hot path of every cd.
	Groups []string
}

// NewMatch runs b's pattern against dir once, refusing the match outright
// when cfg is a confined config and dir does not lie inside its subtree —
// compared with symlinks resolved on both sides wherever dir can be resolved,
// and by file identity where two spellings of one directory would otherwise
// read as two (see newMatch). Everything that builds a Match goes through
// here, so neither "captured against the normalized path" nor the confinement
// rule can end up implemented two different ways.
func NewMatch(cfg *config.Config, b config.Block, dir string) (Match, bool) {
	return newMatch(cfg, b, &candidate{dir: dir})
}

// newMatch is NewMatch over a candidate whose resolution can be shared with
// the other configs and blocks tested against the same directory. It holds
// the confinement refusal and builds the only Match this package ever
// constructs; the exported wrapper exists so no caller outside the package
// can supply a candidate whose resolution it did not compute itself.
func newMatch(cfg *config.Config, b config.Block, c *candidate) (Match, bool) {
	against := c.dir
	// boundByIdentity says the bound still owed is withinBound's, over the
	// resolved path in `against`. It cannot be recovered from `against !=
	// c.dir`: a project with no symlink above it resolves to its own spelling,
	// and that case is still the identity bound rather than the textual
	// fallback below.
	boundByIdentity := false
	if cfg.Local {
		// A confined config's Dir, and the base its ./ patterns were
		// compiled with, are both physical: config.LoadFragmentResolved bases
		// them on the followed link. c.dir is the shell's own $PWD, which is
		// not. Where an ancestor of the project is a symlink
		// the two forms disagree, and comparing or matching one against the
		// other makes every block in the fragment stop firing — on macOS
		// that is any project under /var, a link to private/var.
		//
		// A directory that will not resolve is compared as spelled instead.
		// That loosens nothing: it asks the same question of the same string
		// the bound was always applied to, while the branch above replaces a
		// comparison of spellings with one of facts. Refusing outright would
		// instead cost a leave block for a directory that no longer exists —
		// a build tree removed underfoot, a deleted worktree — and since
		// enter and leave are independent, nothing else would clean up after
		// it.
		if physical, ok := c.resolve(); ok {
			against, boundByIdentity = physical, true
		} else if !Within(c.dir, cfg.Dir) {
			// All that is left to judge by is the spelling: a link in the
			// path could point anywhere, nothing here can tell, and a
			// directory the kernel would not resolve has no file identity to
			// put beside the bound's either.
			return Match{}, false
		}
	}
	// The pattern runs before the identity bound, and the two orders admit the
	// same set: a Match needs both, and neither observes the other. The pattern
	// is chosen first because it is the cheaper refusal by an unbounded margin.
	// A confined fragment's ./ patterns carry cfg.Dir as a literal prefix
	// (config.compilePattern), so for a directory outside the project the regex
	// refuses without a syscall, while withinBound may stat every ancestor up
	// to the filesystem root only to refuse as well. What survives to the bound
	// is a pattern reaching out of its own project — the case the bound exists
	// for, and the one worth the walk.
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
//
// Both are per directory and not per (directory, config, block): collect tests
// every block of every config against the same directory, and both answers
// cost syscalls per path component, which is by far the most expensive thing
// on this path.
type candidate struct {
	dir      string
	physical string
	ok       bool
	resolved bool

	// bounds holds withinBound's answer per base, and only for a base the
	// lexical comparison rejected — nil for every candidate that never needed
	// the walk. Keyed on base, not on the candidate: two confined fragments in
	// one set have different bounds, and one bound is shared by every block of
	// its config and by two links into the same project.
	bounds map[string]bool
}

// resolve returns dir with every symlink followed, computing that at most
// once, and whether it could be determined at all.
//
// ok is false for a directory that no longer exists, or one with a component
// the kernel will not follow. It is never an approximation: a caller gets the
// physical path or is told there isn't one, and decides for itself what to do
// without it.
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
// underneath it: the confinement test for a config bounded to base, over the
// one form of the directory such a config's bound and patterns are both
// expressed in.
//
// The bound is about which directory, not how that directory was spelled. Two
// spellings of one name are one directory wherever the filesystem says so —
// macOS's default, and a mount option on Linux — and Within cannot see that:
// filepath.Rel compares components byte-wise off Windows, and EvalSymlinks
// reproduces the spelling it was handed rather than the one on disk. So a cd
// whose $PWD is cased differently lands in the very directory the fragment
// came with, and the lexical answer is still no.
//
// os.SameFile settles it, and is why this cannot widen the bound: it compares
// the identity of the file each path names, so an ancestor it accepts *is*
// base and the resolved directory is therefore physically inside it. No
// spelling, junction or link can make that untrue, and a link below base has
// already been followed out of it by resolve, taking its ancestors with it. A
// stat that fails leaves no identity to compare, so the answer stays no. Where
// that identity is not something the filesystem reliably keeps, there is no
// settlement to have: sameDirOrAncestor answers no on Windows for that reason,
// leaving the bound there as Within alone.
//
// That soundness rests on the walk starting from the *resolved* path, and
// nothing in the signature says so. Walk the spelled path instead and
// /proj/escape — a link out of the project — has base among its ancestors and
// is admitted. What holds the precondition is that candidate.physical is
// unexported, only resolve writes it, and NewMatch is the only way in from
// outside the package: a refactor that "optimises away" the resolve, or hands
// the walk c.dir, opens containment rather than saving syscalls.
//
// Cost: Within's lexical yes is free, but it says yes only *inside* base, so
// the walk runs for every directory outside a confined fragment's project that
// the fragment's pattern nonetheless matched — which is why newMatch runs the
// pattern first. On Windows it does not run at all, sameDirOrAncestor refusing
// before it stats anything. The memo is still written there: what it records is
// that the identity half was reached and refused, which is what newMatch's
// ordering is judged by, and not that any ancestor was stat'd.
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
// ancestors, by file identity rather than by spelling. On Windows it reports no
// without comparing anything, for the reason at the top of its body — a caller
// there gets a refusal, never an identity answer.
//
// The walk ends at the root, where filepath.Dir is idempotent — the same
// termination ancestors relies on. It costs one stat for base plus one per
// component of dir.
func sameDirOrAncestor(dir, base string) bool {
	// This is the widening half of a security bound — withinBound reaches it
	// only for a directory Within has already placed *outside* base — and on
	// Windows the identity it would widen on is not one every filesystem
	// provides. os.SameFile compares (volume serial, file index) from
	// GetFileInformationByHandle; the file index is documented as unsupported on
	// ReFS, whose 128-bit file IDs do not fit it, and as a directory-entry offset
	// on FAT/exFAT, and some SMB redirectors supply neither. Where two distinct
	// directories report the same one — the same zero, say — os.SameFile calls
	// them one file, and a directory outside the project a confined fragment came
	// with is admitted as if it were inside it.
	//
	// Nothing the walk was written for is lost by refusing. It exists because a
	// filesystem can call one directory by two names, and on Windows both halves
	// of the bound already handle that: EvalSymlinks normalises each component to
	// its on-disk spelling before withinBound sees it — long form, so an 8.3 name
	// too — and filepath.Rel folds component case, which it does on no other
	// platform. What is given up is a directory the kernel reaches through a
	// device mapping no path spells, a subst drive or a drive mapped to a share:
	// that now stays outside the bound, which is the direction a bound is
	// supposed to fail in.
	//
	// So this is not an optimisation standing in front of a working comparison.
	// Deleting it reopens a confinement bypass on filesystems Windows ships.
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

// Within reports whether dir is base or lives underneath it.
//
// This is what keeps a fragment symlinked into a project — a file someone
// else's commit can rewrite — from reaching outside that project, however its
// patterns are written. Trust still gates execution; this bounds what a
// `git pull` can turn an already-approved config into.
//
// filepath.Rel does the work because it already understands the platform's
// path rules: on Windows two different volumes have no relative path at all,
// which is exactly the "not within" answer.
//
// It is arithmetic on strings and touches no filesystem, so two spellings of
// one directory are two directories to it. That makes it the fast half of the
// bound rather than the whole of it — see withinBound — and keeps it usable on
// a path that need not exist, which configset.confine and the fallback in
// newMatch both depend on.
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
// Patterns are regexes over paths and `\` is the regex escape character, so
// they are written with `/`; without this, filepath.Dir's backslashes on
// Windows could never match one. It must stay filepath.ToSlash and never a
// blind ReplaceAll: `\` is a legal character in a Unix filename, where
// rewriting it would corrupt real directory names.
func MatchPath(dir string) string {
	return filepath.ToSlash(dir)
}

// Resolve computes which leave blocks fire walking out of from, and which
// enter blocks fire walking into to, for a directory change from -> to.
//
// from and to must be absolute paths; they're cleaned internally but not
// otherwise resolved. What the shell reported is what a pattern is matched
// against and what a block runs in — with one exception, a confined config,
// whose bound and patterns are physical (see newMatch).
//
// cfgs must be ordered outermost-first — the central config, then each
// envokerc.d fragment in config.Fragments' order, which is what configset.Load
// produces. Resolve applies them in that order for enters and in the reverse
// order for leaves, so a transition unwinds in the order it was applied.
//
// leaves is ordered deepest-directory-first (unwind the nested-most rule
// first, mirroring a stack). enters is ordered shallowest-first (fire the
// outer rule before the nested one on the way in).
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
// This is what `envoke reload` needs and Resolve cannot express. Resolve
// answers "what changed between two directories", so from == to yields
// nothing at all, and passing the root as from would still skip the root
// itself — the one directory that is in every chain.
//
// Leave blocks have no equivalent here on purpose: nothing has been left.
// Re-applying the enters without unwinding anything matches envoke's rule
// that enter and leave are independent and explicit.
func Enters(cfgs []*config.Config, dir string) ([]Match, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("path %q is not absolute", dir)
	}
	return collect(cfgs, ancestors(dir), config.Enter), nil
}

// collect returns every block of the given type matching any of dirs, in
// (directory, config, declaration) order.
//
// The candidate is built in the outer loop so that a directory is resolved at
// most once however many confined configs and blocks are tested against it.
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
