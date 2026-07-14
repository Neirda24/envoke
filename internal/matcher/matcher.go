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
	Dir   string
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

	for _, dir := range left {
		for _, b := range cfg.Blocks {
			if b.Type == config.Leave && b.Pattern.MatchString(dir) {
				leaves = append(leaves, Match{Block: b, Dir: dir})
			}
		}
	}
	for _, dir := range entered {
		for _, b := range cfg.Blocks {
			if b.Type == config.Enter && b.Pattern.MatchString(dir) {
				enters = append(enters, Match{Block: b, Dir: dir})
			}
		}
	}
	return leaves, enters, nil
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
