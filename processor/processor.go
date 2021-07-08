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
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
)

var (
	ErrNotEnoughSpace = errors.New("not enough space to download")
	ErrFinished       = errors.New("finished")
)

const minAvailableSpaceThreshold = uint64(1000 * 1000 * 1000)

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
	plotDirs []string

	// schedule tells us when to check for plots
	schedule map[string]time.Time

	// maxDownloads is the maximum number of parallel downloads
	maxDownloads int
}

func (proc *Processor) getAvailableSpace(plotDir string) (int64, error) {
	available, _, err := disk.GetAvailableSpace(plotDir)
	if err != nil {
		return 0, err
	}

	for _, plot := range proc.plots {
		if plot.GetDownloadDirectory() == "" {
			continue
		}
		if plot.GetDownloadDirectory() == plotDir {
			remaining := plot.GetRemainingBytes()
			available -= uint64(remaining)
		}
	}
	return int64(available), nil
}

func (proc *Processor) getPlotDownloadDirectory(p *plot.Plot) (string, error) {
	// do a first pass to see if any of the directories contains a partial download of the plot.
	// If it does, make sure there's enough space to resume the download. If there is, we'll use
	// this directory. Otherwise, we'll return an error.
	for _, plotDir := range proc.plotDirs {
		fileName := p.GetDownloadFilename()
		filePath := path.Join(plotDir, fileName)

		logrus.Debugf("%s checking if %s exists", p, filePath)

		// continue if the file does not exist (meaning we're not resuming a download in this `plotDir`)
		fInfo, err := os.Stat(filePath)
		if err != nil {
			continue
		}
		remaining := p.GetDownloadSize() - fInfo.Size()

		available, err := proc.getAvailableSpace(plotDir)
		if err != nil {
			return "", err
		}

		if remaining > available {
			log.Errorf("%s there is not enough space in %s to resume the download for %s (%s left to download, available=%s)", proc, plotDir, p.ID, humanize.Bytes(uint64(remaining)), humanize.Bytes(uint64(available)))
			return "", ErrNotEnoughSpace
		}

		log.Infof("%s resuming %s from existing file in %s (available=%s, remaining=%s)", proc, p.ID, plotDir, humanize.Bytes(uint64(available)), humanize.Bytes(uint64(remaining)))
		return plotDir, nil
	}

	// now find the next available directory (with enough space)
	for _, plotDir := range proc.plotDirs {
		available, err := proc.getAvailableSpace(plotDir)
		if err != nil {
			return "", err
		}

		// if there's no room in this directory (drive), continue
		if p.GetDownloadSize() > available {
			log.Warnf("%s %s does not have enough space to download %s (available=%s)", proc, plotDir, p.ID, humanize.Bytes(uint64(available)))
			continue
		}

		log.Infof("%s %s has enough space to start downloading %s (available=%s, plot_size=%s)", proc, plotDir, p.ID, humanize.Bytes(uint64(available)), humanize.Bytes(uint64(p.GetDownloadSize())))
		return plotDir, nil
	}

	log.Errorf("%s none of the provided directories has enough space to download %s", proc, p.ID)
	return "", ErrNotEnoughSpace
}

func (proc *Processor) isDownloadAllowed() bool {
	if proc.maxDownloads == 0 {
		return true
	}

	downloading := 0
	for _, p := range proc.plots {
		if p.State == plot.StatePublished && p.GetDownloadState() != "" {
			downloading++
		}
	}
	return downloading < proc.maxDownloads
}

func (proc *Processor) process(ctx context.Context) (bool, error) {
	for i := range proc.plots {
		p := proc.plots[i]

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

		// by default we'll retrieve the plots from the API every 10 minutes
		nextScheduleTime := now.Add(10 * time.Minute)

		switch p.State {
		case plot.StatePending:
			log.Debugf("%s plotting has not started", p)
		case plot.StatePlotting:
			log.Debugf("%s is currently being plotted (progress=%s)", p, newP.GetPlottingProgress())
			nextScheduleTime = now.Add(1 * time.Minute)
		case plot.StatePublished:
			switch p.GetDownloadState() {
			case plot.DownloadStateLookingForDownloadLocation:
				log.Debugf("%s looking for an available download directory for %s", proc, p.ID)

				plotDir, err := proc.getPlotDownloadDirectory(p)
				if err == ErrNotEnoughSpace {
					log.Errorf("%s please make room to download this plot", p)
					p.SetDownloadError()
				} else if err != nil {
					log.Errorf("%s unexpected error while retrieving verification hashes (%s)", p, err)
					p.SetDownloadError()
				} else {
					p.SetDownloadDirectory(plotDir)
				}
			case plot.DownloadStateWaitingForHashes:
				log.Debugf("%s waiting get the plot verification hashes", p)

				hashList, err := proc.client.GetHashesForPlot(ctx, p.ID)
				if err == client.ErrPlotHashesNotReady {
					log.Warnf("%s verification hashes still not ready. Waiting for chiafactory to calculate them", p)
				} else if err != nil {
					log.Errorf("%s unexpected error while retrieving verification hashes (%s)", p, err)
					p.SetDownloadError()
				} else {
					p.SetFileHashes(hashList)
				}
			case plot.DownloadStateNotStarted:
				go func() {
					if err = p.PrepareDownload(ctx); err != nil {
						p.SetDownloadError()
						log.Errorf("%s error while preparing the download for plot %s. Retrying (error=%s)", proc, p.ID, err.Error())
					}
				}()
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
				log.Debugf("%s downloading (progress=%s)", p, p.GetDownloadProgress())
			case plot.DownloadStateFailed:
				log.Debugf("%s download failed. We'll retry it", p)
				p.RetryDownload(ctx)
			case plot.DownloadStateDownloaded:
				nextScheduleTime = now.Add(1 * time.Minute)
				log.Debugf("%s download finished, marking it as expired", p)
				dp, err := proc.client.DeletePlot(ctx, p.ID)
				if err != nil {
					log.Errorf("%s failed to delete plot (%s). Retrying soon", p, err)
				} else {
					p.UpdateState(dp.State)
				}
			case plot.DownloadStateLiveValidation:
				log.Debugf("%s is validating the latest chunk", p)
			case plot.DownloadStateInitialValidation:
				log.Debugf("%s is validating the last chunk before resuming", p)
			case plot.DownloadStateFailedValidation:
				log.Debugf("%s validation for the last chunk failed. We'll re-download it", p)
				p.RetryDownload(ctx)

			case plot.DownloadStateEnqueued, "":
				if p.DownloadURL == "" && newP.DownloadURL != "" {
					p.DownloadURL = newP.DownloadURL
				}

				if !proc.isDownloadAllowed() {
					p.SetDownloadEnqueued()
					break
				}

				log.Infof("%s initialising download for %s", proc, p.ID)
				if err = p.InitialiseDownload(); err != nil {
					log.Errorf("%s error while initialising the download for plot %s. Retrying (error=%s)", proc, p.ID, err.Error())
					p.SetDownloadError()
				}
			default:
				return false, fmt.Errorf("%s unexpected download state (%s)", proc, p.GetDownloadState())
			}
		case plot.StateCancelled, plot.StateExpired:
			log.Debugf("%s is expired or cancelled", p)
			delete(proc.schedule, p.ID)
			updateSchedule = false
		default:
			return false, fmt.Errorf("unexpected state (%s)", p.State)
		}

		if updateSchedule {
			proc.schedule[p.ID] = nextScheduleTime
			log.Debugf("%s will be checked again at %s", p, nextScheduleTime)
		}
	}

	allDone := true
	for _, p := range proc.plots {
		if p.State != plot.StateCancelled && p.State != plot.StateExpired {
			allDone = false
			break
		}
	}
	if allDone {
		return true, nil
	}

	proc.reporter.render(proc.plots)

	return false, nil
}

func (proc *Processor) Start(ctx context.Context, orderID string) (err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

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

	for _, p := range proc.plots {
		proc.schedule[p.ID] = time.Time{}
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
				// raise warnings about remaining disk space
				for _, plotDir := range proc.plotDirs {
					available, _, err := disk.GetAvailableSpace(plotDir)
					if err != nil {
						log.Warnf("%s error while checking available space in %s: %s", proc, plotDir, err)
						continue
					}

					if available == 0 {
						log.Warnf("%s %s has no remaining space. All downloads will be stopped and the program will exit", proc, plotDir)
						cancel()
						return
					} else if available <= uint64(minAvailableSpaceThreshold) {
						log.Warnf("%s %s is running out of space (remaining=%s)", proc, plotDir, humanize.Bytes(available))
					}
				}

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

func NewProcessor(c *client.Client, r *Reporter, plotDirs []string, frequency time.Duration, maxDownloads int) (*Processor, error) {
	p := &Processor{
		client:       c,
		reporter:     r,
		downloads:    sync.WaitGroup{},
		frequency:    frequency,
		plotDirs:     plotDirs,
		schedule:     map[string]time.Time{},
		maxDownloads: maxDownloads,
	}
	return p, nil
}
