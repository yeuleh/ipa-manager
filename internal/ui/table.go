package ui

import (
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
)

// RenderTable renders rows as a bordered table.
//
// TODO(mission): header styling, column alignment, color by status.
func RenderTable(headers []string, rows [][]string) string {
	t := table.New().
		Border(lipgloss.NormalBorder()).
		Headers(headers...).
		Rows(rows...)
	return t.Render()
}
