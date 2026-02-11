package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	mtime := flag.Bool("m", false, "sort glob results by mtime (newest first)")
	printAll := flag.Bool("n", false, "print all matches, don't invoke editor")
	interactive := flag.Bool("a", false, "interactive file picker")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: edit [flags] <pattern>\n\n")
		fmt.Fprintf(os.Stderr, "Search $EDITPATH directories for files matching pattern and open in $EDITOR.\n\n")
		fmt.Fprintf(os.Stderr, "Patterns:\n")
		fmt.Fprintf(os.Stderr, "  foo.go          simple filename lookup\n")
		fmt.Fprintf(os.Stderr, "  ...go           recursive, files ending in 'go'\n")
		fmt.Fprintf(os.Stderr, "  .../cmd/...go   recursive, dir 'cmd', files ending in 'go'\n")
		fmt.Fprintf(os.Stderr, "  foo.../bar      dirs starting with 'foo', then file 'bar'\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	// Multiple args means the shell already expanded a glob for us.
	// Treat them as literal file paths.
	if flag.NArg() > 1 {
		files := resolveArgs(flag.Args())
		if *mtime {
			sortByMtime(files)
		}
		iter := newSliceIter(files)
		runMode(iter, *interactive, *printAll)
		return
	}

	pattern := flag.Arg(0)

	if strings.HasPrefix(pattern, "/") {
		// Absolute path — use directly
		info, err := os.Stat(pattern)
		if err != nil {
			fmt.Fprintf(os.Stderr, "edit: %v\n", err)
			os.Exit(1)
		}
		if info.IsDir() {
			fmt.Fprintf(os.Stderr, "edit: %s is a directory\n", pattern)
			os.Exit(1)
		}
		if err := invokeEditor(pattern); err != nil {
			fmt.Fprintf(os.Stderr, "edit: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Determine roots and search pattern.
	var roots []string
	var searchPattern string

	if strings.HasPrefix(pattern, "./") {
		// Relative to pwd — use pwd as sole root
		pwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "edit: %v\n", err)
			os.Exit(1)
		}
		roots = []string{pwd}
		searchPattern = strings.TrimPrefix(pattern, "./")
	} else {
		// Search EDITPATH
		editpath := os.Getenv("EDITPATH")
		pwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "edit: %v\n", err)
			os.Exit(1)
		}
		if editpath != "" {
			roots = strings.Split(editpath, ":")
		}
		// Append current directory implicitly
		roots = append(roots, pwd)
		// Resolve all roots to absolute paths and deduplicate
		roots = dedup(roots)
		searchPattern = pattern
	}

	iter, err := newSearchIter(roots, searchPattern, *mtime)
	if err != nil {
		fmt.Fprintf(os.Stderr, "edit: %v\n", err)
		os.Exit(1)
	}
	runMode(iter, *interactive, *printAll)
}

func runMode(iter *searchIter, interactive, printAll bool) {
	if interactive {
		sel, err := runPicker(iter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "edit: %v\n", err)
			os.Exit(1)
		}
		if sel == "" {
			os.Exit(0)
		}
		if err := invokeEditor(sel); err != nil {
			fmt.Fprintf(os.Stderr, "edit: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if printAll {
		found := false
		for {
			path, ok := iter.Next()
			if !ok {
				break
			}
			fmt.Println(path)
			found = true
		}
		if !found {
			fmt.Fprintln(os.Stderr, "no matches")
			os.Exit(1)
		}
		return
	}

	// Default: first match, invoke editor
	path, ok := iter.Next()
	iter.Close()
	if !ok {
		fmt.Fprintln(os.Stderr, "no matches")
		os.Exit(1)
	}
	if err := invokeEditor(path); err != nil {
		fmt.Fprintf(os.Stderr, "edit: %v\n", err)
		os.Exit(1)
	}
}

// resolveArgs converts shell-expanded args to absolute paths, filtering to existing files.
func resolveArgs(args []string) []string {
	var files []string
	for _, a := range args {
		abs, err := filepath.Abs(a)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			continue
		}
		files = append(files, abs)
	}
	return files
}

// dedup resolves all paths to absolute and removes duplicates, preserving order.
func dedup(paths []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if !seen[abs] {
			seen[abs] = true
			out = append(out, abs)
		}
	}
	return out
}
