package processor

import (
	"chiafactory/plotorder/client"
	"chiafactory/plotorder/disk"
	"chiafactory/plotorder/plot"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	log "github.com/sirupsen/logrus"
)

var (
	ErrNotEnoughSpace = errors.New("not enough space to download")
	ErrFinished       = errors.New("finished")
)

type Processor struct {
	// client is the ChiaFactory API client
	client *client.Client

	// reporter will paint the current status of the Processor to stdout
	reporter *Reporter

	// plots is the list of plots the processor is working with
	plots []*plot.Plot

	// downloads is used to keep track of the plots being downloaded
	downloads sync.WaitGroup

	// frequency tells the processor how often to check the state of plots
	frequency time.Duration

	// plotDirs are where plots will be downloaded to. We'll try to fill each
	// location before using the next one
	plotDirs            []string
	claimedBytesByDrive map[string]int64

	// schedule tells us when to check for plots
	schedule map[string]time.Time
}

func (proc *Processor) getPlotDownloadDirectory(p *plot.Plot) (string, error) {
	filename, err := p.GetDownloadFilename()
	if err != nil {
		return "", err
	}

	plotSize, err := p.GetDownloadSize()
	if err != nil {
		return "", err
	}

	// do a first pass to see if any of the directories contains a partial download of the plot.
	// If it does, make sure there's enough space to resume the download. If there is, we'll use
	// this directory. Otherwise, we'll return an error.
	for _, plotDir := range proc.plotDirs {
		filePath := path.Join(plotDir, filename)

		// continue if the file does not exist (meaning we're not resuming a download)
		fInfo, err := os.Stat(filePath)
		if err != nil {
			continue
		}
		remaining := plotSize - fInfo.Size()

		available, drive, err := disk.GetAvailableSpace(plotDir)
		if err != nil {
			return "", err
		}

		// take into account the space claimed by other downloads
		available -= uint64(proc.claimedBytesByDrive[drive])

		if remaining > int64(available) {
			log.Errorf("%s there is not enough space in %s to resume the download for %s (%s left to download)", proc, plotDir, p.ID, humanize.Bytes(uint64(remaining)))
			return "", ErrNotEnoughSpace
		}

		proc.claimedBytesByDrive[drive] += remaining
		return plotDir, nil
	}

	// now find the next available directory (with enough space)
	for _, plotDir := range proc.plotDirs {
		available, drive, err := disk.GetAvailableSpace(plotDir)
		if err != nil {
			return "", err
		}

		// take into account the space claimed by other downloads
		available -= uint64(proc.claimedBytesByDrive[drive])

		// if there's no room in this directory (drive), continue
		if uint64(plotSize) > available {
			log.Warnf("%s %s does not have enough space to download %s (available=%s)", proc, plotDir, p.ID, humanize.Bytes(available))
			continue
		}

		proc.claimedBytesByDrive[drive] += plotSize
		return plotDir, nil
	}

	log.Errorf("%s none of the provided directories has enough space to download %s", proc, p.ID)
	return "", ErrNotEnoughSpace
}

func (proc *Processor) process(ctx context.Context) (bool, error) {
	// keep track of all the expired or cancelled plots
	expiredOrCancelled := 0

	for i := range proc.plots {
		p := proc.plots[i]

		// if it's not
		s, ok := proc.schedule[p.ID]

		// if it's not here, it means we don't have to check any longer
		if !ok {
			continue
		}

		// only reload based on the schedule
		var (
			newP           = p
			err            error
			updateSchedule = false
			now            = time.Now()
		)
		if s.Before(now) {
			newP, err = proc.client.GetPlot(ctx, p.ID)
			if err != nil {
				return false, err
			}
			updateSchedule = true
		}

		if p.State != newP.State {
			p.UpdateState(newP.State)
		}

		if p.PlottingProgress != newP.PlottingProgress {
			p.UpdatePlottingProgress(newP.PlottingProgress)
		}

		if p.DownloadURL != newP.DownloadURL {
			p.UpdateDownloadURL(newP.DownloadURL)
		}

		// by default we'll retrieve the plots from the API every 10 minutes
		nextScheduleTime := now.Add(10 * time.Minute)

		switch p.State {
		case plot.StatePending:
			log.Debugf("%s plotting has not started", p)
		case plot.StatePlotting:
			nextScheduleTime = now.Add(1 * time.Minute)
			log.Debugf("%s is currently being plotted (progress=%s)", p, newP.GetPlottingProgress())
		case plot.StatePublished:
			switch p.DownloadState {
			case plot.DownloadStateNotStarted:

				log.Debugf("%s retrieving verification hashes", p)
				hashList, err := proc.client.GetHashesForPlot(ctx, p.ID)

				// if they are not ready, we will try again
				if err != nil {
					if err == client.ErrPlotHashesNotReady {
						log.Warnf("%s verification hashes still not ready. Waiting for chiafactory to calculate them", p)
					} else {
						log.Errorf("%s unexpected error while retrieving verification hashes (%s)", p, err)
					}
					p.UnableToStartDownload()
					break
				}
				p.FileChunkHashes = hashList

				// if anything goes wrong here, we'll need the user to do something
				plotDir, err := proc.getPlotDownloadDirectory(p)
				if err != nil {
					if err == ErrNotEnoughSpace {
						log.Errorf("%s please make room to download this plot", p)
					} else {
						log.Errorf("%s unexpected error while retrieving verification hashes (%s)", p, err)
					}
					p.UnableToStartDownload()
					break
				}

				go func() {
					p.PrepareDownload(ctx, plotDir)
				}()
			case plot.DownloadStateUnableToStart:
				log.Debugf("%s re-initialising download in a few seconds", p)
				p.InitialiseDownload()
			case plot.DownloadStatePreparing:
				log.Debugf("%s is being prepared for download", p)
			case plot.DownloadStateReady:
				nextScheduleTime = now.Add(10 * time.Minute)
				proc.downloads.Add(1)
				go func() {
					defer proc.downloads.Done()
					p.Download(ctx)
				}()
			case plot.DownloadStateDownloading:
				log.Debugf("%s is still being downloaded (progress=%s)", p, p.GetDownloadProgress())
			case plot.DownloadStateFailed:
				log.Debugf("%s download has failed. We'll retry it", p)
				p.RetryDownload(ctx)
			case plot.DownloadStateDownloaded:
				nextScheduleTime = now.Add(1 * time.Minute)
				log.Debugf("%s download finished, marking it as expired", p)
				dp, err := proc.client.DeletePlot(ctx, p.ID)
				if err != nil {
					log.Errorf("%s failed to delete plot (%s). Retrying soon", p, err)
					continue
				}
				p.UpdateState(dp.State)
			case plot.DownloadStateLiveValidation:
				log.Debugf("%s is validating the latest chunk", p)
			case plot.DownloadStateInitialValidation:
				log.Debugf("%s is validating the last chunk before resuming", p)
			case plot.DownloadStateFailedValidation:
				log.Debugf("%s validation for the last chunk failed. We'll re-download it", p)
				p.RetryDownload(ctx)
			case "":
				log.Infof("%s initialising", p)
				p.InitialiseDownload()
			default:
				return false, fmt.Errorf("%s unexpected download state (%s)", proc, p.DownloadState)
			}
		case plot.StateCancelled, plot.StateExpired:
			expiredOrCancelled++
			delete(proc.schedule, p.ID)
			updateSchedule = false
			log.Debugf("%s is expired or cancelled", p)
		default:
			return false, fmt.Errorf("unexpected state (%s)", p.State)
		}

		if updateSchedule {
			proc.schedule[p.ID] = nextScheduleTime
			log.Debugf("%s will be checked again at %s", p, nextScheduleTime)
		}
	}

	if len(proc.plots) == expiredOrCancelled {
		return true, nil
	}

	proc.reporter.render(proc.plots)

	return false, nil
}

func (proc *Processor) Start(ctx context.Context, orderID string) (err error) {
	ticker := time.NewTicker(proc.frequency)

	order, err := proc.client.GetOrder(ctx, orderID)
	if err != nil {
		return err
	}

	plots, err := proc.client.GetPlotsForOrderID(ctx, order.ID)
	if err != nil {
		return err
	}

	proc.plots = plots

	for _, p := range plots {
		proc.schedule[p.ID] = time.Now()
	}

	log.Infof("%s %s has %d plots", proc, order, len(plots))

	var (
		done     = make(chan struct{})
		finished bool
	)
	go func() {
		defer func() {
			done <- struct{}{}
			ticker.Stop()
		}()
		for {
			select {
			case <-ctx.Done():
				// wait for all the downloads to finish
				proc.downloads.Wait()
				return
			case <-ticker.C:
				finished, err = proc.process(ctx)
				if err != nil {
					// if the context has been cancelled, just continue and
					// we'll capture this in the other case
					if ctx.Err() == context.Canceled {
						continue
					}

					// otherwise, this is an actual error, so return it
					return
				}

				// if we're done, return (eg: all plots already downloaded)
				if finished {
					return
				}
			}
		}
	}()
	<-done
	return err
}

func (proc *Processor) String() string {
	return "[processor]"
}

func NewProcessor(c *client.Client, r *Reporter, plotDirs []string, frequency time.Duration) (*Processor, error) {
	p := &Processor{
		client:              c,
		reporter:            r,
		downloads:           sync.WaitGroup{},
		frequency:           frequency,
		plotDirs:            plotDirs,
		schedule:            map[string]time.Time{},
		claimedBytesByDrive: map[string]int64{},
	}
	return p, nil
}
