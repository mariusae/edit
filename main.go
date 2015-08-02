// Edit is a "fuzzy" file finder/plumber for Acme.
//
// Usage:
//
// 	edit query [dirs...]
//
// Edit executes a query against a set of directories (default: .). If there
// are two or fewer results, edit will automatically plumb the files,
// similar to Plan 9's B command.
//
// Edit traverses each given directory, skipping common database paths
// (.git, .svn), and matches each entry against the query.
//
// A candidate path is admitted if all the characters in the query appear
// in the same order in the path. If the query contains the path separator
// '/' then the characters in the query delimited by that separator must
// appear within a single path element in the candidate path.
package main // import "marius.ae/edit"

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
)

var ignoreDirs = map[string]bool{
	".git": true,
	".svn": true,
}

var printOnly = flag.Bool("n", false, "Don't plumb results, just print them.")
var editOnly = flag.Bool("e", false, "Force edit, regardless of number of hits.")

func usage() {
	fmt.Fprintf(os.Stderr, "usage: edit query [dir...]\n")
	fmt.Fprint(os.Stderr, "options:\n")
	flag.PrintDefaults()
	os.Exit(2)
}

func match1(query, path string) bool {
	for _, c := range query {
		i := strings.IndexRune(path, c)
		if i < 0 {
			return false
		}
		path = path[i:]
	}

	return true
}

func match(query, path string) bool {
	ps := strings.Split(path, "/")
	qs := strings.Split(query, "/")
	i := 0

	for _, q := range qs {
		found := false
		for !found && i < len(ps) {
			found = match1(q, ps[i])
			i++
		}
		if !found {
			return false
		}
	}

	return true
}

func plumb(path string) {
	out, err := exec.Command("plumb", "-d", "edit", path).CombinedOutput()
	if err != nil {
		log.Fatalf("plumb: %v\n%s", err, out)
	}
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("edit: ")
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() < 1 || (*printOnly && *editOnly) {
		usage()
	}

	query := flag.Arg(0)
	dirs := flag.Args()[1:]
	if len(dirs) == 0 {
		dirs = []string{"."}
	}

	cased := false
	for _, r := range query {
		cased = cased || unicode.IsUpper(r)
	}

	if !cased {
		query = strings.ToLower(query)
	}

	matches := []string{}

	for _, d := range dirs {
		filepath.Walk(d, func(path string, info os.FileInfo, err error) error {
			fi, err := os.Stat(path)
			if err != nil {
				return err
			}

			if !fi.Mode().IsRegular() {
				if _, ok := ignoreDirs[filepath.Base(path)]; ok {
					return filepath.SkipDir
				}
				return nil
			}

			rel, err := filepath.Rel(d, path)
			if err != nil {
				return err
			}

			if !cased {
				rel = strings.ToLower(rel)
			}

			if match(query, rel) {
				matches = append(matches, path)
			}
			return nil
		})
	}

	if len(matches) < 3 && !*printOnly || *editOnly {
		for _, path := range matches {
			plumb(path)
		}
	} else {
		for _, path := range matches {
			fmt.Println(path)
		}
	}

}