package processor

import (
	"chiafactory/plotorder/client"
	"chiafactory/plotorder/plot"
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type Processor struct {
	client    *client.Client
	plots     []*plot.Plot
	downloads sync.WaitGroup

	// frequency tells the processor how often to check the state of plots
	frequency time.Duration

	// plotDir is where plots will be downloaded to
	plotDir string
}

func (proc *Processor) process(ctx context.Context) error {
	for i := range proc.plots {
		p := proc.plots[i]

		// always reload
		newP, err := proc.client.GetPlot(ctx, p.ID)
		if err != nil {
			return err
		}

		// and update state and progres if necessary
		if p.State != newP.State {
			p.UpdateState(newP.State)
		} else if p.PlottingProgress != newP.PlottingProgress {
			p.UpdatePlottingProgress(newP.PlottingProgress)
		}

		switch p.State {
		case plot.StatePending:
			log.Debug("%s has not started plotting", p)
		case plot.StatePlotting:
			log.Debug("%s is currently being plotted (progress=%d%%)", p, newP.PlottingProgress)
		case plot.StatePublished:
			switch p.DownloadState {
			case plot.DownloadStateNotStarted:
				proc.downloads.Add(1)
				go func() {
					defer proc.downloads.Done()
					p.Download(ctx, proc.plotDir)
				}()
			case plot.DownloadStateDownloading:
				log.Debug("%s is still being downloaded", p)
			case plot.DownloadStateFailed:
				log.Debug("%s download has failed", p)
			case plot.DownloadStateDownloaded:
				proc.client.DeletePlot(ctx, p.ID)
			default:
				return fmt.Errorf("unexpected download state (%s)", p.DownloadState)
			}
		case plot.StateCancelled, plot.StateExpired:
			// ignore
		default:
			return fmt.Errorf("unexpected state (%s)", p.State)
		}
	}

	writeReport(proc.plots)

	return nil
}

func (proc *Processor) Start(ctx context.Context, orderID string) (chan struct{}, error) {
	ticker := time.NewTicker(proc.frequency)

	order, err := proc.client.GetOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}

	plots, err := proc.client.GetPlotsForOrderID(ctx, order.ID)
	if err != nil {
		return nil, err
	}

	proc.plots = plots

	log.Infof("order (%s) has %d plots", orderID, len(plots))

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ctx.Done():
				// stop the ticker, we're done at this point
				ticker.Stop()

				// wait for all the downloads to finish
				proc.downloads.Wait()

				// we're done now
				done <- struct{}{}
				return
			case <-ticker.C:
				err = proc.process(ctx)
				if err != nil {
					// if the context has been cancelled, just continue and
					// we'll capture this in the other case
					if ctx.Err() == context.Canceled {
						continue
					}

					// otherwise, this is an actual error, so return it
					return
				}
			}
		}
	}()
	return done, nil
}

func NewProcessor(c *client.Client, plotDir string, frequency time.Duration) (*Processor, error) {
	if _, err := os.Stat(plotDir); err != nil {
		log.Warnf("%s does not exist, creating it", plotDir)
		if err = os.Mkdir(plotDir, os.ModePerm); err != nil {
			return nil, errors.Wrap(err, "failed to create plot download directory")
		}
	}

	p := &Processor{
		client:    c,
		downloads: sync.WaitGroup{},
		frequency: frequency,
		plotDir:   plotDir,
	}
	return p, nil
}
