// Edit is a file finder/plumber for Acme.
//
// Usage:
//
// 	edit query [dirs...]
//
// Edit executes a query against a set of directories (default: .).
// If there is exactly one result, edit will automatically plumb the
// files, similar to Plan 9's B command.
//
// The EDITPATH environment variable is a colon-separated list of
// directories to look for files.
//
// Using the invocation:
//
//	edit dir:query
//
// Edit executes the query against  x/dir for every directory x in EDITPATH.
//
// Edit traverses each given directory, skipping common database paths
// (.git, .svn), and matches each entry against the query.
//
// Queries are partial paths. A query matches a candidate path 
// when each path element in the query matches a path element
// in the candidate path. The elements have to appear in the same
// order, but not all path elements from the candidate path are
// required to match.
//
// A query path element matches a candidate path element if 
// (1) it is a substring of the path element; or (2) it is a glob pattern
// (containing any of "*?[") that matches according to filepath.Match.
package main // import "marius.ae/edit"

// 	- Scoring/select first

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

func match1(q, p string) bool {
	if strings.IndexAny(q, "*?[") > -1 {
		ok, _ := filepath.Match(q, p)
		return ok
	} else {
		return strings.Index(p, q) > -1
	}
}

func match(query, path string) bool {
	ps := strings.Split(path, "/")
	qs := strings.Split(query, "/")
	i := 0

	for _, q := range qs[:len(qs)-1] {
		found := false
		for !found && i < len(ps)-1 {
			found = match1(q, ps[i])
			i++
		}
		if !found {
			return false
		}
	}

	p := ps[len(ps)-1]
	q := qs[len(qs)-1]

	return match1(q, p)
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
	cased := false
	for _, r := range query {
		cased = cased || unicode.IsUpper(r)
	}

	if !cased {
		query = strings.ToLower(query)
	}

	var dirs []string
	if strings.Contains(query, ":") && flag.NArg() == 1 {
		i := strings.Index(query, ":")
		var path string
		path, query = query[0:i], query[i+1:]

		dirs = filepath.SplitList(os.Getenv("EDITPATH"))
		for i := range dirs {
			dirs[i] = filepath.Join(dirs[i], path)
		}
	} else {
		if flag.NArg() == 1 {
			dirs = []string{"."}
		} else {

			dirs = flag.Args()[1:]
		}
	}

	//	log.Printf("query \"%s\" dirs \"%v\"", query, dirs)

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

	if len(matches) == 1 && !*printOnly || *editOnly {
		for _, path := range matches {
			plumb(path)
		}
	} else {
		for _, path := range matches {
			fmt.Println(path)
		}
	}

}