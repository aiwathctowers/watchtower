package ui

import (
	"log"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
)

var renderer *glamour.TermRenderer

func init() {
	style := styles.DarkStyleConfig
	style.H2.StylePrimitive.Prefix = ""
	style.H3.StylePrimitive.Prefix = ""
	style.H4.StylePrimitive.Prefix = ""
	style.H5.StylePrimitive.Prefix = ""
	style.H6.StylePrimitive.Prefix = ""
	margin := uint(0)
	style.Document.Margin = &margin

	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(0),
	)
	if err != nil {
		log.Printf("warning: failed to create markdown renderer: %v", err)
		return
	}
	renderer = r
}

// RenderMarkdown renders markdown text for terminal display using glamour.
// Falls back to raw text on error.
func RenderMarkdown(text string) string {
	if renderer == nil {
		return text
	}
	out, err := renderer.Render(text)
	if err != nil {
		return text
	}
	return out
}
