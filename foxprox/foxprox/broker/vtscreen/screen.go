// Package vtscreen maintains a small virtual terminal grid for broker-owned
// PTYs.
//
// This is intentionally a strict subset of a full VT emulator: printable
// UTF-8, CR/LF/backspace, common CSI cursor movement, erase-display/line, SGR
// ignore, and alt-screen enter/exit. It is enough to turn noisy TUI redraws
// into readable text snapshots while keeping the broker dependency surface
// small.
package vtscreen

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// Cursor is the rendered cursor position in zero-based coordinates.
type Cursor struct {
	Row     int  `json:"row"`
	Col     int  `json:"col"`
	Visible bool `json:"visible"`
}

// Snapshot is an immutable screen render.
type Snapshot struct {
	Rows      int      `json:"rows"`
	Cols      int      `json:"cols"`
	Lines     []string `json:"lines"`
	DirtyRows []int    `json:"dirty_rows,omitempty"`
	Cursor    Cursor   `json:"cursor"`
	AltScreen bool     `json:"alt_screen,omitempty"`
}

// Screen is a concurrency-safe virtual terminal screen.
type Screen struct {
	mu sync.Mutex

	rows int
	cols int

	primary [][]rune
	alt     [][]rune
	useAlt  bool

	row int
	col int

	state parserState
	buf   []byte

	dirty map[int]struct{}
}

type parserState uint8

const (
	stateGround parserState = iota
	stateEscape
	stateCSI
)

// New returns a screen with the supplied dimensions. Zero dimensions use
// 40x120, matching the session PTY defaults.
func New(rows, cols uint16) *Screen {
	r, c := int(rows), int(cols)
	if r <= 0 {
		r = 40
	}
	if c <= 0 {
		c = 120
	}
	s := &Screen{rows: r, cols: c, dirty: make(map[int]struct{})}
	s.primary = makeGrid(r, c)
	s.alt = makeGrid(r, c)
	s.markAllDirty()
	return s
}

// Resize updates the grid dimensions, preserving overlapping content.
func (s *Screen) Resize(rows, cols uint16) {
	r, c := int(rows), int(cols)
	if r <= 0 || c <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r == s.rows && c == s.cols {
		return
	}
	s.primary = resizeGrid(s.primary, r, c)
	s.alt = resizeGrid(s.alt, r, c)
	s.rows, s.cols = r, c
	if s.row >= s.rows {
		s.row = s.rows - 1
	}
	if s.col >= s.cols {
		s.col = s.cols - 1
	}
	s.markAllDirty()
}

// Feed applies PTY output bytes to the screen.
func (s *Screen) Feed(b []byte) {
	if len(b) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < len(b); {
		c := b[i]
		if s.state != stateGround {
			s.stepControl(c)
			i++
			continue
		}
		switch c {
		case 0x1B:
			s.state = stateEscape
			i++
		case '\r':
			s.col = 0
			i++
		case '\n':
			s.newline()
			i++
		case '\b':
			if s.col > 0 {
				s.col--
			}
			i++
		case '\t':
			next := ((s.col / 8) + 1) * 8
			for s.col < next {
				s.putRune(' ')
			}
			i++
		default:
			if c < 0x20 {
				i++
				continue
			}
			r, size := utf8.DecodeRune(b[i:])
			if r == utf8.RuneError && size == 1 {
				r = rune(c)
			}
			s.putRune(r)
			i += size
		}
	}
}

// Snapshot returns a copy of the current rendered screen and clears dirty rows.
func (s *Screen) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	lines := make([]string, s.rows)
	grid := s.grid()
	for i := 0; i < s.rows; i++ {
		lines[i] = strings.TrimRight(string(grid[i]), " ")
	}
	dirty := make([]int, 0, len(s.dirty))
	for row := range s.dirty {
		dirty = append(dirty, row)
	}
	sort.Ints(dirty)
	clear(s.dirty)
	return Snapshot{
		Rows:      s.rows,
		Cols:      s.cols,
		Lines:     lines,
		DirtyRows: dirty,
		Cursor:    Cursor{Row: s.row, Col: s.col, Visible: true},
		AltScreen: s.useAlt,
	}
}

func (s *Screen) stepControl(c byte) {
	switch s.state {
	case stateEscape:
		if c == '[' {
			s.state = stateCSI
			s.buf = s.buf[:0]
			return
		}
		s.state = stateGround
	case stateCSI:
		if c >= 0x40 && c <= 0x7E {
			s.finishCSI(c)
			s.state = stateGround
			s.buf = s.buf[:0]
			return
		}
		if len(s.buf) < 64 {
			s.buf = append(s.buf, c)
		}
	}
}

func (s *Screen) finishCSI(final byte) {
	params := string(s.buf)
	switch final {
	case 'H', 'f':
		parts := splitParams(params)
		row, col := 1, 1
		if len(parts) >= 1 && parts[0] > 0 {
			row = parts[0]
		}
		if len(parts) >= 2 && parts[1] > 0 {
			col = parts[1]
		}
		s.moveTo(row-1, col-1)
	case 'A':
		s.row -= firstParam(params, 1)
		s.clampCursor()
	case 'B':
		s.row += firstParam(params, 1)
		s.clampCursor()
	case 'C':
		s.col += firstParam(params, 1)
		s.clampCursor()
	case 'D':
		s.col -= firstParam(params, 1)
		s.clampCursor()
	case 'J':
		s.eraseDisplay(firstParam(params, 0))
	case 'K':
		s.eraseLine(firstParam(params, 0))
	case 'h', 'l':
		s.applyPrivateMode(params, final == 'h')
	case 'm':
		// SGR only affects styling; this first snapshot engine stores text.
	}
}

func (s *Screen) applyPrivateMode(params string, enable bool) {
	if !strings.HasPrefix(params, "?") {
		return
	}
	for _, p := range strings.Split(strings.TrimPrefix(params, "?"), ";") {
		switch p {
		case "47", "1047", "1049":
			if s.useAlt != enable {
				s.useAlt = enable
				s.row, s.col = 0, 0
				if enable {
					clearGrid(s.alt)
				}
				s.markAllDirty()
			}
		}
	}
}

func (s *Screen) putRune(r rune) {
	if s.row < 0 || s.row >= s.rows || s.col < 0 || s.col >= s.cols {
		s.clampCursor()
	}
	s.grid()[s.row][s.col] = r
	s.dirty[s.row] = struct{}{}
	s.col++
	if s.col >= s.cols {
		s.col = 0
		s.newline()
	}
}

func (s *Screen) newline() {
	s.row++
	if s.row < s.rows {
		return
	}
	grid := s.grid()
	copy(grid[0:], grid[1:])
	grid[s.rows-1] = makeBlankRow(s.cols)
	s.row = s.rows - 1
	s.markAllDirty()
}

func (s *Screen) moveTo(row, col int) {
	s.row, s.col = row, col
	s.clampCursor()
}

func (s *Screen) clampCursor() {
	if s.row < 0 {
		s.row = 0
	}
	if s.row >= s.rows {
		s.row = s.rows - 1
	}
	if s.col < 0 {
		s.col = 0
	}
	if s.col >= s.cols {
		s.col = s.cols - 1
	}
}

func (s *Screen) eraseDisplay(mode int) {
	grid := s.grid()
	switch mode {
	case 2, 3:
		clearGrid(grid)
		s.markAllDirty()
	default:
		for r := s.row; r < s.rows; r++ {
			start := 0
			if r == s.row {
				start = s.col
			}
			clearRunes(grid[r][start:])
			s.dirty[r] = struct{}{}
		}
	}
}

func (s *Screen) eraseLine(mode int) {
	row := s.grid()[s.row]
	switch mode {
	case 1:
		clearRunes(row[:s.col+1])
	case 2:
		clearRunes(row)
	default:
		clearRunes(row[s.col:])
	}
	s.dirty[s.row] = struct{}{}
}

func (s *Screen) grid() [][]rune {
	if s.useAlt {
		return s.alt
	}
	return s.primary
}

func (s *Screen) markAllDirty() {
	for i := 0; i < s.rows; i++ {
		s.dirty[i] = struct{}{}
	}
}

func makeGrid(rows, cols int) [][]rune {
	grid := make([][]rune, rows)
	for i := range grid {
		grid[i] = makeBlankRow(cols)
	}
	return grid
}

func resizeGrid(old [][]rune, rows, cols int) [][]rune {
	grid := makeGrid(rows, cols)
	for r := 0; r < rows && r < len(old); r++ {
		copy(grid[r], old[r])
	}
	return grid
}

func makeBlankRow(cols int) []rune {
	row := make([]rune, cols)
	clearRunes(row)
	return row
}

func clearGrid(grid [][]rune) {
	for _, row := range grid {
		clearRunes(row)
	}
}

func clearRunes(row []rune) {
	for i := range row {
		row[i] = ' '
	}
}

func splitParams(params string) []int {
	if params == "" {
		return nil
	}
	parts := strings.Split(params, ";")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			out = append(out, 0)
			continue
		}
		v, err := strconv.Atoi(p)
		if err != nil {
			out = append(out, 0)
			continue
		}
		out = append(out, v)
	}
	return out
}

func firstParam(params string, def int) int {
	parts := splitParams(params)
	if len(parts) == 0 || parts[0] == 0 {
		return def
	}
	return parts[0]
}
