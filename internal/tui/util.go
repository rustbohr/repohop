package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// cell renders a value as exactly width display cells: cut when it is too
// long, padded when it is too short. Columns are built from this rather than
// from pad alone, because a value wider than its column would otherwise push
// everything after it out of line — which is what happens the first time a
// real branch name turns up longer than the column it was given.
func cell(value string, width int) string {
	return pad(truncate(value, width), width)
}

// pad right-pads a string to width display cells.
func pad(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// truncate shortens a string to width display cells, ending with an ellipsis
// when anything was cut. Styling is preserved only when it does not need
// cutting, which is enough for the places this is used.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(stripANSI(s))
	if width == 1 {
		return "…"
	}
	if len(runes) > width-1 {
		runes = runes[:width-1]
	}
	return string(runes) + "…"
}

// stripANSI removes escape sequences so a truncated string cannot end mid-code.
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inEscape := false
	for _, r := range s {
		switch {
		case inEscape:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
		case r == '\x1b':
			inEscape = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// plural renders a count with its unit, e.g. "1 repo" / "4 repos" /
// "2 repositories".
func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(n) + " " + pluralise(unit)
}

// pluralise covers the handful of words repohop actually counts.
func pluralise(unit string) string {
	switch {
	case strings.HasSuffix(unit, "y"):
		return strings.TrimSuffix(unit, "y") + "ies"
	case strings.HasSuffix(unit, "ch"), strings.HasSuffix(unit, "sh"),
		strings.HasSuffix(unit, "s"), strings.HasSuffix(unit, "x"):
		return unit + "es"
	default:
		return unit + "s"
	}
}

// columnWidth sizes a column to its widest value, within the bounds given.
// Values wider than the maximum are cut by cell, not allowed to widen the
// column.
func columnWidth(values []string, minWidth, maxWidth int) int {
	width := minWidth
	for _, value := range values {
		if w := lipgloss.Width(value); w > width {
			width = w
		}
	}
	if width > maxWidth {
		width = maxWidth
	}
	if width < 1 {
		width = 1
	}
	return width
}

// share is a fraction of the terminal width, used to bound a column against
// the space actually available rather than an arbitrary constant.
func share(width, numerator, denominator, minimum int) int {
	if denominator <= 0 {
		return minimum
	}
	return max(width*numerator/denominator, minimum)
}

// itoa is strconv.Itoa under a shorter name; the render paths use it a lot.
func itoa(n int) string { return strconv.Itoa(n) }
