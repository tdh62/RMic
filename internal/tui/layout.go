package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	footerButton   = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("236"))
	footerShortcut = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Background(lipgloss.Color("236"))
	footerSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("235")).Background(lipgloss.Color("115")).Bold(true)
)

// footerControl uses color and padding instead of punctuation as a button frame.
// Both states occupy exactly the same cells, including the shortcut label.
func footerControl(label string, selected bool, width int) string {
	label = fitText(label, max(1, width-2))
	padding := strings.Repeat(" ", max(0, width-2-lipgloss.Width(label)))
	if selected {
		return footerSelected.Render(" " + label + padding + " ")
	}
	name, key := label, ""
	if split := strings.LastIndex(label, " "); split > 0 {
		tail := label[split+1:]
		if len(tail) > 0 && tail[0] < 128 {
			name, key = label[:split], label[split:]
		}
	}
	return footerButton.Render(" "+name) + footerShortcut.Render(key) + footerButton.Render(padding+" ")
}

// Buttons keep the same text and geometry for every focus position. Packing is
// based only on terminal width; no action is replaced by a different action.
func actionBar(labels []string, selected, width int) string {
	width = max(12, width-2)
	columns := min(len(labels), max(2, (width+2)/18))
	if len(labels) == 0 {
		return ""
	}
	cellWidth := (width - 2*(columns-1)) / columns
	var rows []string
	var items []string
	for i, label := range labels {
		items = append(items, clickable("action:"+strconv.Itoa(i), footerControl(label, i == selected, cellWidth)))
		if len(items) == columns || i == len(labels)-1 {
			rows = append(rows, strings.Join(items, "  "))
			items = nil
		}
	}
	return strings.Join(rows, "\n")
}

// fitText keeps list rows on one line; the selected item has its own detail row.
func fitText(text string, width int) string {
	if lipgloss.Width(text) <= width {
		return text
	}
	var result strings.Builder
	for _, r := range text {
		if lipgloss.Width(result.String()+string(r))+1 > max(1, width) {
			break
		}
		result.WriteRune(r)
	}
	return result.String() + "…"
}

// page keeps the controls at the bottom and scrolls the body around its focus.
func page(body []string, footer string, width, height, focusLine int) string {
	text, _, _ := layoutPage(body, footer, width, height, focusLine, true)
	return text
}

// scrolledPage uses a viewport offset rather than moving the keyboard focus.
func (f *mouseFrame) scrolledPage(body []string, footer string, width, height, offset int) string {
	text, start, limit := layoutPage(body, footer, width, height, offset, false)
	if f != nil {
		f.scrollPosition, f.scrollLimit = start, limit
	}
	return text
}

func layoutPage(body []string, footer string, width, height, position int, followFocus bool) (string, int, int) {
	footer = mouseHints(footer)
	width = max(12, width-2)
	wrapped := strings.Split(lipgloss.NewStyle().Width(width).Render(strings.Join(body, "\n")), "\n")
	bottom := lipgloss.NewStyle().Width(width).Render(dim.Render(footer))
	capacity := max(1, height-strings.Count(bottom, "\n")-2)
	if followFocus {
		position -= capacity - 2
	}
	limit := max(0, len(wrapped)-capacity)
	start := max(0, min(position, limit))
	end := min(len(wrapped), start+capacity)
	return strings.Join(wrapped[start:end], "\n") + strings.Repeat("\n", max(1, capacity-(end-start)+1)) + bottom, start, limit
}
