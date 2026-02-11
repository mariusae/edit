package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type segmentKind int

const (
	segWild      segmentKind = iota // pattern possibly containing "..." (wildcard within name)
	segRecursive                    // standalone "..." — matches 0+ directory levels
)

type segment struct {
	kind    segmentKind
	pattern string // only for segWild
}

// matchWild checks whether name matches a pattern where "..." acts as a
// wildcard matching any substring. If the pattern contains no "...", it
// requires an exact match.
func matchWild(pattern, name string) bool {
	parts := strings.Split(pattern, "...")
	if len(parts) == 1 {
		// No "..." — exact match.
		return pattern == name
	}

	// First part must be a prefix.
	if !strings.HasPrefix(name, parts[0]) {
		return false
	}
	rest := name[len(parts[0]):]

	// Last part must be a suffix.
	last := parts[len(parts)-1]
	if !strings.HasSuffix(rest, last) {
		return false
	}
	rest = rest[:len(rest)-len(last)]

	// Middle parts must appear in order.
	for _, mid := range parts[1 : len(parts)-1] {
		idx := strings.Index(rest, mid)
		if idx < 0 {
			return false
		}
		rest = rest[idx+len(mid):]
	}
	return true
}

// wildPrefix returns the fixed prefix of a pattern before the first "...".
// If no "..." exists, returns the whole pattern and false.
func wildPrefix(pattern string) (string, bool) {
	idx := strings.Index(pattern, "...")
	if idx < 0 {
		return pattern, false
	}
	return pattern[:idx], true
}

func parsePattern(pattern string) ([]segment, error) {
	parts := strings.Split(pattern, "/")
	var segments []segment
	for _, p := range parts {
		if p == "" {
			continue
		}
		if p == "..." {
			segments = append(segments, segment{kind: segRecursive})
		} else {
			segments = append(segments, segment{kind: segWild, pattern: p})
		}
	}
	if len(segments) == 0 {
		return nil, fmt.Errorf("empty pattern")
	}
	// Trailing "..." means "match all files recursively" — append a
	// match-everything wildcard leaf.
	if segments[len(segments)-1].kind == segRecursive {
		segments = append(segments, segment{kind: segWild, pattern: "..."})
	}

	// Implicit recursion: if the last segment starts with "..." but is not
	// standalone "...", insert a recursive segment before it. This makes
	// leaf patterns like "...go" automatically recursive, so that
	// ".../internal/...go" matches files at any depth under "internal".
	last := len(segments) - 1
	if segments[last].kind == segWild && strings.HasPrefix(segments[last].pattern, "...") {
		if last == 0 || segments[last-1].kind != segRecursive {
			segments = append(segments, segment{})
			copy(segments[last+1:], segments[last:])
			segments[last] = segment{kind: segRecursive}
		}
	}

	return segments, nil
}

type searchConfig struct {
	sortByMtime bool
	exhaustive  bool
}

// Search finds files matching the pattern across the given root directories.
// Results are sent on the results channel, which is closed when the search is done.
func Search(roots []string, pattern string, cfg searchConfig, results chan<- string) {
	defer close(results)

	segments, err := parsePattern(pattern)
	if err != nil {
		fmt.Fprintf(os.Stderr, "edit: %v\n", err)
		return
	}

	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		found := matchSegments(root, segments, cfg, results)
		if found && !cfg.exhaustive {
			return
		}
	}
}

// matchSegments recursively matches path segments starting from base.
// Returns true if at least one result was emitted.
func matchSegments(base string, segs []segment, cfg searchConfig, results chan<- string) bool {
	if len(segs) == 0 {
		return false
	}

	// Last segment: match files
	if len(segs) == 1 {
		return matchLeaf(base, segs[0], cfg, results)
	}

	seg := segs[0]
	rest := segs[1:]

	switch seg.kind {
	case segRecursive:
		// Try matching remaining segments starting from current base
		found := matchSegments(base, rest, cfg, results)
		if found && !cfg.exhaustive {
			return true
		}
		// Walk subdirectories (sorted lex), recurse with same ... + remaining
		dirs := listDirs(base)
		for _, d := range dirs {
			sub := filepath.Join(base, d)
			found = matchSegments(sub, segs, cfg, results) || found // keep "..." in play
			if found && !cfg.exhaustive {
				return true
			}
		}
		return found

	case segWild:
		if !strings.Contains(seg.pattern, "...") {
			// Exact segment — use os.Stat directly (O(1) vs listing the directory).
			candidate := filepath.Join(base, seg.pattern)
			info, err := os.Stat(candidate)
			if err != nil || !info.IsDir() {
				return false
			}
			return matchSegments(candidate, rest, cfg, results)
		}

		// Wildcard segment — list the directory and filter.
		prefix, _ := wildPrefix(seg.pattern)
		entries, err := os.ReadDir(base)
		if err != nil {
			return false
		}
		found := false
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			if prefix != "" && !strings.HasPrefix(name, prefix) {
				continue
			}
			if !matchWild(seg.pattern, name) {
				continue
			}
			sub := filepath.Join(base, name)
			if matchSegments(sub, rest, cfg, results) {
				found = true
				if !cfg.exhaustive {
					return true
				}
			}
		}
		return found
	}

	return false
}

// matchLeaf matches files in base against the leaf segment pattern.
func matchLeaf(base string, seg segment, cfg searchConfig, results chan<- string) bool {
	if seg.kind == segRecursive {
		// shouldn't happen due to validation, but be safe
		return false
	}

	if !strings.Contains(seg.pattern, "...") {
		// Exact filename — use os.Stat directly.
		candidate := filepath.Join(base, seg.pattern)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			return false
		}
		results <- candidate
		return true
	}

	// Wildcard leaf — list directory and filter.
	prefix, _ := wildPrefix(seg.pattern)
	entries, err := os.ReadDir(base)
	if err != nil {
		return false
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		if matchWild(seg.pattern, name) {
			files = append(files, filepath.Join(base, name))
		}
	}

	if len(files) == 0 {
		return false
	}

	if cfg.sortByMtime {
		sortByMtime(files)
	} else {
		sort.Strings(files)
	}

	for _, f := range files {
		results <- f
		if !cfg.exhaustive {
			return true
		}
	}
	return true
}

// listDirs returns sorted directory names within base, excluding hidden dirs.
func listDirs(base string) []string {
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	return dirs
}

// sortByMtime sorts file paths by modification time, newest first.
func sortByMtime(files []string) {
	sort.Slice(files, func(i, j int) bool {
		si, ei := os.Stat(files[i])
		sj, ej := os.Stat(files[j])
		if ei != nil || ej != nil {
			return files[i] < files[j]
		}
		return si.ModTime().After(sj.ModTime())
	})
}
