package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var printOnly = flag.Bool("n", false, "Don't plumb results, just print them.")
var editOnly = flag.Bool("e", false, "Force edit, regardless of number of hits.")

func usage() {
	fmt.Fprintf(os.Stderr, "usage: edit query [dir...]\n")
	fmt.Fprint(os.Stderr, "options:\n")
	flag.PrintDefaults()
	os.Exit(2)
}

/*

TODO: "smart" case.

*/

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

	query = strings.ToLower(query)

	matches := []string{}

	for _, d := range dirs {
		filepath.Walk(d, func(path string, info os.FileInfo, err error) error {
			fi, err := os.Stat(path)
			if err != nil {
				return err
			}

			if !fi.Mode().IsRegular() {
				return nil
			}

			rel, err := filepath.Rel(d, path)
			if err != nil {
				return err
			}

			if match(query, strings.ToLower(rel)) {
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