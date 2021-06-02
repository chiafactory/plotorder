package processor

import (
	"chiafactory/plotorder/plot"
	"fmt"
	"os"

	"github.com/dustin/go-humanize"
	"github.com/olekukonko/tablewriter"
)

const (
	pendingColour   = tablewriter.FgYellowColor
	plottingColour  = tablewriter.FgBlueColor
	publishedColour = tablewriter.FgGreenColor
	expiredColour   = tablewriter.FgCyanColor
	cancelledColour = tablewriter.FgCyanColor
	unknownColour   = tablewriter.BgRedColor
)

func addRowWithColour(table *tablewriter.Table, row []string, colour int) {
	colours := []tablewriter.Colors{}
	for i := 0; i < len(row); i++ {
		colours = append(colours, tablewriter.Colors{colour})
	}
	table.Rich(row, colours)
}

func formatDownloadSpeed(bytesPerSecond uint64) string {
	return fmt.Sprintf("%s/s", humanize.Bytes(bytesPerSecond))
}

func writeReport(plots []*plot.Plot) {
	fmt.Print("\033[H\033[2J")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Plot", "State", "Filename", "Progress", "Speed"})
	table.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
	table.SetCenterSeparator("|")

	for _, p := range plots {
		switch p.State {
		case plot.StatePending:
			addRowWithColour(table, []string{p.ID, "Pending", "", "", "N/A"}, pendingColour)
		case plot.StatePlotting:
			addRowWithColour(table, []string{p.ID, "Plotting", "", "", "N/A"}, plottingColour)
		case plot.StatePublished:
			switch p.DownloadState {
			case plot.DownloadStateNotStarted:
				addRowWithColour(table, []string{p.ID, "Download pending", "", "", "N/A"}, publishedColour)
			case plot.DownloadStateDownloading:
				addRowWithColour(table, []string{p.ID, "Downloading", p.GetDownloadLocalPath(), fmt.Sprintf("%d%%", p.DownloadProgress), formatDownloadSpeed(p.GetDownloadSpeed())}, publishedColour)
			case plot.DownloadStateFailed:
				addRowWithColour(table, []string{p.ID, "Download failed", "", "", "N/A"}, publishedColour)
			case plot.DownloadStateDownloaded:
				addRowWithColour(table, []string{p.ID, "Downloaded", p.GetDownloadLocalPath(), fmt.Sprintf("%d%%", p.DownloadProgress), formatDownloadSpeed(p.GetDownloadSpeed())}, publishedColour)
			default:
				addRowWithColour(table, []string{p.ID, "<unknown>", "", "", "N/A"}, unknownColour)
			}
		case plot.StateCancelled:
			addRowWithColour(table, []string{p.ID, "Cancelled", "", "", "N/A"}, cancelledColour)
		case plot.StateExpired:
			addRowWithColour(table, []string{p.ID, "Expired", "", "", "N/A"}, expiredColour)
		default:
			addRowWithColour(table, []string{p.ID, "<unknown>", "", "", "N/A"}, unknownColour)
		}
	}
	table.Render()
}
