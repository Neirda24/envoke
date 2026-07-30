// Package matcher resolves which enter/leave config blocks fire for a
// directory change, including ondir-style traverse behavior: jumping
// straight from /a to /a/x/y/z still fires the rules for the intermediate
// directories /a/x and /a/x/y, not just the destination.
package matcher

import (
	"fmt"
	"path/filepath"

	"github.com/Neirda24/envoke/internal/config"
)

// Match is a single fired rule: which block matched, and the directory it
// matched against (not necessarily from or to — it may be an intermediate
// directory on the path between them).
type Match struct {
	Block config.Block
	// Dir is the directory in the platform's own form (backslashes on
	// Windows), because it is used as a working directory and handed to
	// scripts as ENVOKE_DIR.
	Dir string
	// Groups holds the pattern's submatches, Groups[0] being the whole
	// match, captured against the slash-normalized form of Dir (see
	// MatchPath). Storing them means the pattern runs once per candidate
	// directory instead of once to test and again to extract — matching is
	// on the hot path of every single `cd`.
	Groups []string
}

// NewMatch runs b's pattern against dir once, returning the resulting Match
// and whether it matched at all. Everything that builds a Match goes through
// here so the "captured against the normalized path" rule can't be
// implemented differently in two places.
func NewMatch(b config.Block, dir string) (Match, bool) {
	groups := b.Pattern.FindStringSubmatch(MatchPath(dir))
	if groups == nil {
		return Match{}, false
	}
	return Match{Block: b, Dir: dir, Groups: groups}, true
}

// MatchPath is the form of a path that patterns are matched against:
// forward-slash separated, whatever the platform uses natively.
//
// Config patterns are written with `/` — they're regexes over paths, and
// `\` is the regex escape character, so nobody writes a Windows-style
// pattern. Without this, filepath.Dir's backslash output on Windows could
// never match any pattern a user would plausibly write, which made the
// whole matching engine a no-op there. filepath.ToSlash is deliberately the
// mechanism rather than a blind ReplaceAll: `\` is a perfectly legal
// character in a Unix filename, and rewriting it there would corrupt real
// directory names.
func MatchPath(dir string) string {
	return filepath.ToSlash(dir)
}

// Resolve computes which leave blocks fire walking out of from, and which
// enter blocks fire walking into to, for a directory change from -> to.
//
// from and to must be absolute paths; they're cleaned internally but not
// otherwise resolved (no symlink evaluation).
//
// leaves is ordered deepest-directory-first (unwind the nested-most rule
// first, mirroring a stack). enters is ordered shallowest-first (fire the
// outer rule before the nested one on the way in).
func Resolve(cfg *config.Config, from, to string) (leaves, enters []Match, err error) {
	left, entered, err := Transitions(from, to)
	if err != nil {
		return nil, nil, err
	}

	leaves = collect(cfg, left, config.Leave)
	enters = collect(cfg, entered, config.Enter)
	return leaves, enters, nil
}

// collect returns every block of the given type matching any of dirs, in
// (directory, declaration) order.
func collect(cfg *config.Config, dirs []string, want config.BlockType) []Match {
	var matches []Match
	for _, dir := range dirs {
		for _, b := range cfg.Blocks {
			if b.Type != want {
				continue
			}
			if m, ok := NewMatch(b, dir); ok {
				matches = append(matches, m)
			}
		}
	}
	return matches
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
