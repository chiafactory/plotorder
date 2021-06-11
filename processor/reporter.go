package processor

import (
	"chiafactory/plotorder/plot"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/fatih/color"
	"github.com/gosuri/uilive"
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

const (
	StatePending           = "Pending"
	StatePlotting          = "Plotting"
	StateDownloadPending   = "Download pending"
	StateDownloadPreparing = "Preparing download"
	StateDownloadReady     = "Ready to download"
	StateDownloading       = "Downloading"
	StateDownloadFailed    = "Download failed"
	StateDownloaded        = "Downloaded"
	StateValidatingChunk   = "Validating (resumes shortly)"
	StateCancelled         = "Cancelled"
	StateExpired           = "Expired"
	StateUnknown           = "<unknown>"
)

// the entries in the table will be sorted based on the 'State' column, following
// the order in this slice
var statesForTableOrder = []string{
	StateDownloading,
	StatePlotting,
	StatePending,
	StateDownloadPending,
	StateDownloadPreparing,
	StateDownloadReady,
	StateValidatingChunk,
	StateDownloadFailed,
	StateDownloaded,
	StateExpired,
	StateCancelled,
	StateUnknown,
}

var (
	cyan    = color.New(color.FgCyan)
	yellow  = color.New(color.FgYellow)
	magenta = color.New(color.FgMagenta)
	blue    = color.New(color.FgBlue)
	green   = color.New(color.FgGreen)
)

func printSectionTitle(writer io.Writer, title string) {
	fmt.Fprintf(writer, "\n- %s\n\n", title)
}

type row struct {
	data   []string
	colour int
}

func NewReporter() *Reporter {
	w := uilive.New()
	w.RefreshInterval = 500 * time.Millisecond
	s := time.Now()
	return &Reporter{
		w: w,
		s: s,
	}
}

type Reporter struct {
	w             *uilive.Writer
	s             time.Time
	disableStdout bool
}

func (r *Reporter) Write(b []byte) (n int, err error) {
	if r.disableStdout {
		return len(b), nil
	}
	fmt.Printf(string(b))
	return len(b), nil
}

func (r *Reporter) Start() {
	r.w.Start()
}

func (r *Reporter) Stop() {
	r.w.Stop()
}

func (r *Reporter) render(plots []*plot.Plot) {
	// disable stdout writes in the first render
	if !r.disableStdout {
		r.disableStdout = true
	}

	now := time.Now()
	elapsed := now.Sub(r.s).Round(time.Second)

	tableOrder := map[string]int{}
	for idx, status := range statesForTableOrder {
		tableOrder[status] = idx
	}

	rows := []row{}
	table := tablewriter.NewWriter(r.w)
	table.SetHeader([]string{"Plot", "State", "Progress", "Speed"})
	table.SetAutoFormatHeaders(false)
	table.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
	table.SetCenterSeparator("+")
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
			rows = append(rows, row{[]string{p.ID, StatePlotting, p.GetPlottingProgress(), "-"}, plottingColour})
		case plot.StatePublished:
			switch p.DownloadState {
			case plot.DownloadStateNotStarted:
				downloading++
				rows = append(rows, row{[]string{p.ID, StateDownloadPending, "-", "-"}, publishedColour})
			case plot.DownloadStateReady:
				downloading++
				rows = append(rows, row{[]string{p.ID, StateDownloadReady, "-", "-"}, publishedColour})
			case plot.DownloadStatePreparing:
				downloading++
				rows = append(rows, row{[]string{p.ID, StateDownloadPreparing, "-", "-"}, publishedColour})
			case plot.DownloadStateDownloading:
				downloading++
				rows = append(rows, row{[]string{p.ID, StateDownloading, p.GetDownloadProgress(), p.GetDownloadSpeed()}, publishedColour})
			case plot.DownloadStateFailed:
				downloading++
				rows = append(rows, row{[]string{p.ID, StateDownloadFailed, "-", "-"}, publishedColour})
			case plot.DownloadStateValidatingChunk:
				downloading++
				rows = append(rows, row{[]string{p.ID, StateValidatingChunk, "-", "-"}, publishedColour})
			case plot.DownloadStateDownloaded:
				rows = append(rows, row{[]string{p.ID, StateDownloaded, p.GetDownloadProgress(), p.GetDownloadSpeed()}, publishedColour})
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

		if aidx == bidx {
			return rows[i].data[0] < rows[j].data[0]
		}

		return aidx < bidx
	})

	for _, r := range rows {
		table.Rich(r.data, []tablewriter.Colors{[]int{r.colour}})
	}

	printSectionTitle(r.w, "Summary")
	fmt.Fprintf(r.w, "* Elapsed: %s\n", elapsed)
	r.w.Newline()

	fmt.Fprintf(r.w, "* Total plots: %d\n", len(plots))
	yellow.Fprintf(r.w, "  * Pending: %d\n", pending)
	magenta.Fprintf(r.w, "  * Expired: %d\n", expired)
	magenta.Fprintf(r.w, "  * Cancelled: %d\n", cancelled)
	blue.Fprintf(r.w, "  * Plotting: %d\n", plotting)
	green.Fprintf(r.w, "  * Downloading: %d\n", downloading)

	r.w.Newline()
	printSectionTitle(r.w, "Downloading and plotting")
	table.Render()

	r.w.Newline()
	fmt.Fprint(r.w, "\n")
	fmt.Fprint(r.w, "Press \"q + ENTER\" or \"Ctrl+C\" to exit. Downloads will resume if you restart.\n")
	r.w.Flush()
}
