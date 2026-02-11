package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

var brailleFrames = [...]rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

type picker struct {
	allResults []string // absolute paths
	filtered   []int    // indices into allResults matching current search
	search     string
	selected   int // index into filtered
	offset     int // scroll offset into filtered
	mu         sync.Mutex
	maxVisible int
	searching  bool
	pwd        string
	spinFrame  int
}

func newPicker(pwd string) *picker {
	return &picker{
		maxVisible: 10,
		searching:  true,
		pwd:        pwd,
	}
}

func (p *picker) displayPath(abs string) string {
	rel, err := filepath.Rel(p.pwd, abs)
	if err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return abs
}

func (p *picker) addResult(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.allResults = append(p.allResults, path)
	// Add to filtered set if it matches the current search.
	if p.matches(path) {
		p.filtered = append(p.filtered, len(p.allResults)-1)
	}
}

func (p *picker) searchDone() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.searching = false
}

// matches reports whether path matches the current search. Must be called
// with p.mu held.
func (p *picker) matches(path string) bool {
	if p.search == "" {
		return true
	}
	dp := p.displayPath(path)
	return strings.Contains(strings.ToLower(dp), strings.ToLower(p.search))
}

// setSearch updates the search string, rebuilds the filtered set, and selects
// the first match. Returns false if no results match (keystroke rejected).
func (p *picker) setSearch(s string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s != "" {
		lower := strings.ToLower(s)
		var filtered []int
		for i, r := range p.allResults {
			dp := p.displayPath(r)
			if strings.Contains(strings.ToLower(dp), lower) {
				filtered = append(filtered, i)
			}
		}
		if len(filtered) == 0 && !p.searching {
			return false
		}
		p.search = s
		p.filtered = filtered
		p.selected = 0
	} else {
		p.search = ""
		p.rebuildFiltered()
	}
	p.offset = 0
	p.clampOffset()
	return true
}

// rebuildFiltered rebuilds the filtered list with no search active (all items).
// Must be called with p.mu held.
func (p *picker) rebuildFiltered() {
	p.filtered = p.filtered[:0]
	for i := range p.allResults {
		p.filtered = append(p.filtered, i)
	}
}

func (p *picker) clampOffset() {
	if p.selected < p.offset {
		p.offset = p.selected
	}
	if p.selected >= p.offset+p.maxVisible {
		p.offset = p.selected - p.maxVisible + 1
	}
	if p.offset < 0 {
		p.offset = 0
	}
}

func (p *picker) moveUp() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.selected > 0 {
		p.selected--
		p.clampOffset()
	}
}

func (p *picker) moveDown() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.selected < len(p.filtered)-1 {
		p.selected++
		p.clampOffset()
	}
}

func (p *picker) getSelection() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.filtered) == 0 {
		return ""
	}
	return p.allResults[p.filtered[p.selected]]
}

// wantMore returns true when the picker needs more results to fill the
// visible area or stay ahead of the scroll position. Must be called with
// p.mu held.
func (p *picker) wantMore() bool {
	return p.selected+p.maxVisible >= len(p.filtered)
}

// render draws the picker list. Cursor is assumed at line 0, col 0 of the
// display area on entry and is returned there on exit.
func (p *picker) render() {
	p.mu.Lock()
	defer p.mu.Unlock()

	end := p.offset + p.maxVisible
	if end > len(p.filtered) {
		end = len(p.filtered)
	}

	linesDown := 0
	for i := p.offset; i < end; i++ {
		if linesDown > 0 {
			fmt.Fprint(os.Stderr, "\r\n")
		}
		dp := p.displayPath(p.allResults[p.filtered[i]])
		fmt.Fprint(os.Stderr, highlightLine(dp, p.search, i == p.selected))
		fmt.Fprint(os.Stderr, "\033[K")
		linesDown++
	}

	if p.searching {
		if linesDown > 0 {
			fmt.Fprint(os.Stderr, "\r\n")
		}
		fmt.Fprintf(os.Stderr, "\033[2m%c\033[0m\033[K", brailleFrames[p.spinFrame%len(brailleFrames)])
		linesDown++
	}

	// Clear any leftover lines from a previous longer render.
	fmt.Fprint(os.Stderr, "\033[J")

	// Move cursor back to the first line.
	if linesDown > 1 {
		fmt.Fprintf(os.Stderr, "\033[%dA", linesDown-1)
	}
	fmt.Fprint(os.Stderr, "\r")
}

// clear removes the picker display.
func (p *picker) clear() {
	fmt.Fprint(os.Stderr, "\r\033[J")
}

// highlightLine renders a display path with the search match highlighted.
// All occurrences of search in dp are highlighted.
func highlightLine(dp, search string, isSelected bool) string {
	if search == "" {
		if isSelected {
			return "\033[7m" + dp + "\033[0m"
		}
		return dp
	}

	lower := strings.ToLower(dp)
	searchLower := strings.ToLower(search)

	var b strings.Builder
	if isSelected {
		b.WriteString("\033[7m")
	}

	i := 0
	for i < len(dp) {
		idx := strings.Index(lower[i:], searchLower)
		if idx < 0 {
			b.WriteString(dp[i:])
			break
		}
		// Text before the match.
		b.WriteString(dp[i : i+idx])
		// Highlighted match.
		if isSelected {
			b.WriteString("\033[1;4;7m")
		} else {
			b.WriteString("\033[1;33m")
		}
		b.WriteString(dp[i+idx : i+idx+len(search)])
		// Restore.
		if isSelected {
			b.WriteString("\033[0;7m")
		} else {
			b.WriteString("\033[0m")
		}
		i += idx + len(search)
	}

	b.WriteString("\033[0m")
	return b.String()
}

// runPicker runs the interactive picker and returns the selected file path,
// or empty string if cancelled. Returns an error if no results are available.
func runPicker(iter *searchIter) (string, error) {
	// Wait for at least one result before showing the picker.
	first, ok := iter.Next()
	if !ok {
		return "", fmt.Errorf("no matches")
	}

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("interactive picker requires a terminal")
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return "", fmt.Errorf("failed to set raw mode: %w", err)
	}
	defer term.Restore(fd, oldState)

	pwd, _ := os.Getwd()
	p := newPicker(pwd)
	p.allResults = append(p.allResults, first)
	p.filtered = []int{0}

	type keyEvent struct {
		b   []byte
		err error
	}
	keyCh := make(chan keyEvent, 1)
	go func() {
		buf := make([]byte, 32)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				keyCh <- keyEvent{nil, err}
				return
			}
			b := make([]byte, n)
			copy(b, buf[:n])
			keyCh <- keyEvent{b, nil}
		}
	}()

	redraw := func() {
		fmt.Fprint(os.Stderr, "\r")
		p.render()
	}

	redraw()

	iterDone := false
	search := ""

	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	for {
		// Determine whether to pull more results from the iterator.
		var pullCh <-chan string
		p.mu.Lock()
		needMore := p.wantMore()
		p.mu.Unlock()
		if !iterDone && needMore {
			pullCh = iter.ch
		}

		select {
		case path, ok := <-pullCh:
			if !ok {
				iterDone = true
				p.searchDone()
				ticker.Stop()
				redraw()
			} else {
				p.addResult(path)
				redraw()
			}

		case <-ticker.C:
			if !iterDone {
				p.mu.Lock()
				p.spinFrame++
				p.mu.Unlock()
				redraw()
			}

		case ev := <-keyCh:
			if ev.err != nil {
				p.clear()
				iter.Close()
				return "", ev.err
			}
			b := ev.b

			switch {
			case len(b) == 1 && b[0] == 27: // Escape
				p.clear()
				iter.Close()
				return "", nil

			case len(b) == 1 && b[0] == 13: // Enter
				sel := p.getSelection()
				p.clear()
				iter.Close()
				return sel, nil

			case len(b) == 1 && (b[0] == 127 || b[0] == 8): // Backspace
				if len(search) > 0 {
					search = search[:len(search)-1]
					p.setSearch(search)
					redraw()
				}

			case len(b) == 3 && b[0] == 27 && b[1] == 91 && b[2] == 65: // Up
				p.moveUp()
				redraw()

			case len(b) == 3 && b[0] == 27 && b[1] == 91 && b[2] == 66: // Down
				p.moveDown()
				redraw()

			case len(b) == 1 && b[0] >= 32 && b[0] < 127: // Printable
				candidate := search + string(b[0])
				if p.setSearch(candidate) {
					search = candidate
					redraw()
				}
				// Otherwise: reject the character (no match).
			}
		}
	}
}

func invokeEditor(path string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		return fmt.Errorf("$EDITOR is not set; set it to your preferred editor (e.g., export EDITOR=vim)")
	}
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
