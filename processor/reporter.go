package processor

import (
	"chiafactory/plotorder/plot"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
)

const (
	pendingColour   = tablewriter.FgYellowColor
	plottingColour  = tablewriter.FgBlueColor
	publishedColour = tablewriter.FgGreenColor
	expiredColour   = tablewriter.FgMagentaColor
	cancelledColour = tablewriter.FgMagentaColor
	unknownColour   = tablewriter.BgRedColor
)

type row struct {
	data   []string
	colour int
}

const (
	StatePending         = "Pending"
	StatePlotting        = "Plotting"
	StateDownloadPending = "Download pending"
	StateDownloading     = "Downloading"
	StateDownloadFailed  = "Download failed"
	StateDownloaded      = "Downloaded"
	StateValidatingChunk = "Validating"
	StateCancelled       = "Cancelled"
	StateExpired         = "Expired"
	StateUnknown         = "<unknown>"
)

// the entries in the table will be sorted based on the 'State' column, following
// the order in this slice
var statesForTableOrder = []string{
	StateDownloading,
	StatePlotting,
	StatePending,
	StateDownloadPending,
	StateDownloadFailed,
	StateDownloaded,
	StateExpired,
	StateCancelled,
	StateUnknown,
}

func formatDownloadSpeed(bytesPerSecond int64) string {
	return fmt.Sprintf("%s/s", humanize.Bytes(uint64(bytesPerSecond)))
}

func printSectionTitle(writer io.Writer, title string) {
	fmt.Fprint(writer, "------------------------------\n")
	fmt.Fprintf(writer, "%s\n", title)
	fmt.Fprint(writer, "------------------------------\n")

}

func NewReporter() *Reporter {
	return &Reporter{stdoutEnabled: true}
}

type Reporter struct {
	stdoutEnabled   bool
	pendingLogLines []string
}

func (r *Reporter) Write(b []byte) (n int, err error) {
	// write to stdout when needed
	if r.stdoutEnabled {
		os.Stdout.Write(b)
	}

	if len(r.pendingLogLines) >= 10 {
		copy(r.pendingLogLines[0:], r.pendingLogLines[1:])
		r.pendingLogLines[len(r.pendingLogLines)-1] = ""
		r.pendingLogLines = r.pendingLogLines[:len(r.pendingLogLines)-1]
	}
	r.pendingLogLines = append(r.pendingLogLines, string(b))
	return len(b), nil
}

func (r *Reporter) render(plots []*plot.Plot) {
	// as soon as we call render, disable stdout
	if r.stdoutEnabled {
		r.stdoutEnabled = false
	}

	tableOrder := map[string]int{}
	for idx, status := range statesForTableOrder {
		tableOrder[status] = idx
	}

	fmt.Print("\033[H\033[2J")

	rows := []row{}

	out := &strings.Builder{}

	table := tablewriter.NewWriter(out)
	table.SetHeader([]string{"Plot", "State", "Progress", "Speed"})
	table.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
	table.SetCenterSeparator("|")
	table.SetColMinWidth(0, 10)
	table.SetColMinWidth(1, 15)
	table.SetColMinWidth(2, 10)
	table.SetColMinWidth(3, 10)
	table.SetColumnAlignment([]int{tablewriter.ALIGN_CENTER, tablewriter.ALIGN_CENTER, tablewriter.ALIGN_CENTER, tablewriter.ALIGN_CENTER})

	var (
		pending     = 0
		downloading = 0
		plotting    = 0
		cancelled   = 0
		expired     = 0
		unknown     = 0
	)

	for _, p := range plots {
		switch p.State {
		case plot.StatePending:
			pending++
		case plot.StatePlotting:
			plotting++
			rows = append(rows, row{[]string{p.ID, StatePlotting, fmt.Sprintf("%d%%", p.PlottingProgress), "N/A"}, plottingColour})
		case plot.StatePublished:
			switch p.DownloadState {
			case plot.DownloadStateNotStarted:
				rows = append(rows, row{[]string{p.ID, StateDownloadPending, "", "N/A"}, publishedColour})
			case plot.DownloadStateDownloading:
				downloading++
				rows = append(rows, row{[]string{p.ID, StateDownloading, fmt.Sprintf("%.2f%%", p.GetDownloadProgress()), formatDownloadSpeed(p.GetDownloadSpeed())}, publishedColour})
			case plot.DownloadStateFailed:
				rows = append(rows, row{[]string{p.ID, StateDownloadFailed, "", "N/A"}, publishedColour})
			case plot.DownloadStateDownloaded:
				rows = append(rows, row{[]string{p.ID, StateDownloaded, fmt.Sprintf("%.2f%%", p.GetDownloadProgress()), formatDownloadSpeed(p.GetDownloadSpeed())}, publishedColour})
			case plot.DownloadStateVadidatingChunk:
				rows = append(rows, row{[]string{p.ID, StateValidatingChunk, "N/A", "N/A"}, publishedColour})
			default:
				unknown++
			}
		case plot.StateCancelled:
			cancelled++
		case plot.StateExpired:
			expired++
		default:
			unknown++
		}
	}

	// sort the table rows
	sort.Slice(rows, func(i, j int) bool {
		a := rows[i].data[1]
		b := rows[j].data[1]

		aidx := tableOrder[a]
		bidx := tableOrder[b]

		return aidx < bidx
	})

	for _, r := range rows {
		table.Rich(r.data, []tablewriter.Colors{[]int{r.colour}})
	}

	cyan := color.New(color.FgCyan)
	yellow := color.New(color.FgYellow)
	magenta := color.New(color.FgMagenta)
	blue := color.New(color.FgBlue)
	green := color.New(color.FgGreen)

	printSectionTitle(out, "Summary")

	cyan.Fprintf(out, "All plots: %d\n", len(plots))
	yellow.Fprintf(out, "  * Pending plots: %d\n", pending)
	magenta.Fprintf(out, "  * Expired plots: %d\n", expired)
	magenta.Fprintf(out, "  * Cancelled plots: %d\n", cancelled)
	blue.Fprintf(out, "  * Plotting: %d\n", plotting)
	green.Fprintf(out, "  * Downloading: %d\n", downloading)
	out.WriteString("\n")

	printSectionTitle(out, "Downloading and plotting")
	table.Render()

	out.WriteString("\n")
	printSectionTitle(out, "Logs")
	for _, line := range r.pendingLogLines {
		out.WriteString(line)
	}
	out.WriteString("\n")
	out.WriteString(`Press "q + ENTER" or "Ctrl+C" to exit. Downloads will resume if you restart.`)
	fmt.Println(out.String())
}
