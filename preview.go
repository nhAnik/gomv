package gomv

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/hexops/gotextdiff"
)

var (
	bold  = color.New(color.Bold).PrintfFunc()
	red   = color.New(color.FgRed).PrintfFunc()
	green = color.New(color.FgGreen).PrintfFunc()
	blue  = color.New(color.FgBlue).PrintfFunc()
)

// previewDiff previews diff in a colorful format.
func previewDiff(diff gotextdiff.Unified) {
	bold("--- %s\n", diff.From)
	bold("+++ %s\n", diff.To)

	for _, hunk := range diff.Hunks {
		plus, minus := 0, 0
		for _, line := range hunk.Lines {
			switch line.Kind {
			case gotextdiff.Delete:
				minus++
			case gotextdiff.Insert:
				plus++
			case gotextdiff.Equal:
				minus++
				plus++
			}
		}
		blue("@@ -%d,%d +%d,%d @@\n", hunk.FromLine, minus, hunk.ToLine, plus)
		for _, line := range hunk.Lines {
			switch line.Kind {
			case gotextdiff.Delete:
				red("-%s", line.Content)
			case gotextdiff.Insert:
				green("+%s", line.Content)
			case gotextdiff.Equal:
				fmt.Printf(" %s", line.Content)
			}
		}
	}
}
