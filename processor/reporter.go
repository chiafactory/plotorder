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
	errorColour     = tablewriter.FgRedColor
)

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
	sortKey int
	data    []string
	colour  int
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

	rows := []row{}
	table := tablewriter.NewWriter(r.w)
	table.SetHeader([]string{"Plot", "State", "Progress", "Speed", "Download Directory"})
	table.SetAutoFormatHeaders(false)
	table.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
	table.SetCenterSeparator("+")
	table.SetColMinWidth(0, 10)
	table.SetColMinWidth(1, 30)
	table.SetColMinWidth(2, 10)
	table.SetColMinWidth(3, 10)
	table.SetColMinWidth(3, 15)
	table.SetColumnAlignment([]int{tablewriter.ALIGN_CENTER, tablewriter.ALIGN_CENTER, tablewriter.ALIGN_CENTER, tablewriter.ALIGN_CENTER, tablewriter.ALIGN_CENTER})

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
			rows = append(rows, row{1, []string{p.ID, "Plotting", p.GetPlottingProgress(), "-", "-"}, plottingColour})
		case plot.StatePublished:
			downloading++

			if p.HasDownloadError() {
				rows = append(rows, row{0, []string{p.ID, "Error, please check logs", "-", "-", "-"}, errorColour})
				continue
			}

			switch p.GetDownloadState() {
			case plot.DownloadStateNotStarted:
				rows = append(rows, row{0, []string{p.ID, "Download pending", "-", "-", p.GetDownloadDirectory()}, publishedColour})
			case plot.DownloadStateReady:
				rows = append(rows, row{0, []string{p.ID, "Ready to download", "-", "-", p.GetDownloadDirectory()}, publishedColour})
			case plot.DownloadStatePreparing:
				rows = append(rows, row{0, []string{p.ID, "Preparing download", "-", "-", p.GetDownloadDirectory()}, publishedColour})
			case plot.DownloadStateInitialValidation:
				rows = append(rows, row{0, []string{p.ID, "Validating before resuming", "-", "-", p.GetDownloadDirectory()}, publishedColour})
			case plot.DownloadStateDownloading:
				rows = append(rows, row{0, []string{p.ID, "Downloading", p.GetDownloadProgress(), p.GetDownloadSpeed(), p.GetDownloadDirectory()}, publishedColour})
			case plot.DownloadStateFailed:
				rows = append(rows, row{0, []string{p.ID, "Download failed", "-", "-", p.GetDownloadDirectory()}, publishedColour})
			case plot.DownloadStateFailedValidation:
				rows = append(rows, row{0, []string{p.ID, "Validation failed, re-downloading", "-", "-", p.GetDownloadDirectory()}, publishedColour})
			case plot.DownloadStateLiveValidation:
				rows = append(rows, row{0, []string{p.ID, "Downloading (and validating)", p.GetDownloadProgress(), p.GetDownloadSpeed(), p.GetDownloadDirectory()}, publishedColour})
			case plot.DownloadStateDownloaded:
				rows = append(rows, row{0, []string{p.ID, "Downloaded", p.GetDownloadProgress(), p.GetDownloadSpeed(), p.GetDownloadDirectory()}, publishedColour})
			case plot.DownloadStateLookingForDownloadLocation:
				rows = append(rows, row{0, []string{p.ID, "Looking for download location", "-", "-", p.GetDownloadDirectory()}, publishedColour})
			case plot.DownloadStateWaitingForHashes:
				rows = append(rows, row{0, []string{p.ID, "Waiting for hashes", "-", "-", p.GetDownloadDirectory()}, publishedColour})
			case plot.DownloadStateEnqueued:
				rows = append(rows, row{0, []string{p.ID, "Download enqueued", "-", "-", p.GetDownloadDirectory()}, publishedColour})
			default:
				rows = append(rows, row{0, []string{p.ID, "Pending", "-", "-", "-"}, publishedColour})
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
		a := rows[i].sortKey
		b := rows[j].sortKey

		if a == b {
			return rows[i].data[0] < rows[j].data[0]
		}

		return a < b
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
