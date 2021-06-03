package processor

import (
	"chiafactory/plotorder/plot"
	"fmt"
	"sort"
	"strings"

	"github.com/dustin/go-humanize"
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

func writeReport(plots []*plot.Plot) {
	tableOrder := map[string]int{}
	for idx, status := range statesForTableOrder {
		tableOrder[status] = idx
	}

	fmt.Print("\033[H\033[2J")

	rows := []row{}

	tableStr := &strings.Builder{}
	tableStr.WriteString("\n")

	table := tablewriter.NewWriter(tableStr)
	table.SetHeader([]string{"Plot", "State", "Progress", "Speed"})
	table.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
	table.SetCenterSeparator("|")
	table.SetColMinWidth(0, 10)
	table.SetColMinWidth(1, 15)
	table.SetColMinWidth(2, 10)
	table.SetColMinWidth(3, 10)
	table.SetColumnAlignment([]int{tablewriter.ALIGN_CENTER, tablewriter.ALIGN_CENTER, tablewriter.ALIGN_CENTER, tablewriter.ALIGN_CENTER})

	for _, p := range plots {
		switch p.State {
		case plot.StatePending:
			rows = append(rows, row{[]string{p.ID, StatePending, "", "N/A"}, pendingColour})
		case plot.StatePlotting:
			rows = append(rows, row{[]string{p.ID, StatePlotting, fmt.Sprintf("%d%%", p.PlottingProgress), "N/A"}, plottingColour})
		case plot.StatePublished:
			switch p.DownloadState {
			case plot.DownloadStateNotStarted:
				rows = append(rows, row{[]string{p.ID, StateDownloadPending, "", "N/A"}, publishedColour})
			case plot.DownloadStateDownloading:
				rows = append(rows, row{[]string{p.ID, StateDownloading, fmt.Sprintf("%.2f%%", p.GetDownloadProgress()), formatDownloadSpeed(p.GetDownloadSpeed())}, publishedColour})
			case plot.DownloadStateFailed:
				rows = append(rows, row{[]string{p.ID, StateDownloadFailed, "", "N/A"}, publishedColour})
			case plot.DownloadStateDownloaded:
				rows = append(rows, row{[]string{p.ID, StateDownloaded, fmt.Sprintf("%.2f%%", p.GetDownloadProgress()), formatDownloadSpeed(p.GetDownloadSpeed())}, publishedColour})
			default:
				rows = append(rows, row{[]string{p.ID, StateUnknown, "", "N/A"}, unknownColour})
			}
		case plot.StateCancelled:
			rows = append(rows, row{[]string{p.ID, StateCancelled, "", "N/A"}, cancelledColour})
		case plot.StateExpired:
			rows = append(rows, row{[]string{p.ID, StateExpired, "", "N/A"}, expiredColour})
		default:
			rows = append(rows, row{[]string{p.ID, StateUnknown, "", "N/A"}, unknownColour})
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

	table.Render()
	tableStr.WriteString("\n")
	tableStr.WriteString(`Press "q + ENTER" or "Ctrl+C" to exit. Downloads will resume if you restart.`)
	fmt.Println(tableStr.String())
}
