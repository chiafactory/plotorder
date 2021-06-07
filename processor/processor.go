package processor

import (
	"chiafactory/plotorder/client"
	"chiafactory/plotorder/plot"
	"context"
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
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

	// plotDir is where plots will be downloaded to
	plotDir string

	// schedule tells us when to check for plots
	schedule map[string]time.Time
}

func (proc *Processor) process(ctx context.Context) error {
	for i := range proc.plots {
		p := proc.plots[i]

		// if it's not
		s, ok := proc.schedule[p.ID]

		// if it's not here, it means we don't have to check any longer
		if !ok {
			continue
		}

		// only check if we're past the scheduled time
		if s.After(time.Now()) {
			continue
		}

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

		// by default we'll check every 10 minutes
		proc.schedule[p.ID] = s.Add(10 * time.Minute)

		switch p.State {
		case plot.StatePending:
			log.Debug("%s has not started plotting", p)
		case plot.StatePlotting:
			log.Debug("%s is currently being plotted (progress=%d%%)", p, newP.PlottingProgress)
		case plot.StatePublished:
			proc.schedule[p.ID] = s.Add(5 * time.Second)
			switch p.DownloadState {
			case plot.DownloadStateNotStarted:
				proc.downloads.Add(1)
				go func() {
					defer proc.downloads.Done()
					p.Download(ctx, proc.plotDir, []string{})
				}()
			case plot.DownloadStateDownloading:
				log.Debug("%s is still being downloaded", p)
			case plot.DownloadStateFailed:
				log.Debug("%s download has failed", p)
				delete(proc.schedule, p.ID)
			case plot.DownloadStateDownloaded:
				proc.client.DeletePlot(ctx, p.ID)
				delete(proc.schedule, p.ID)
			default:
				return fmt.Errorf("unexpected download state (%s)", p.DownloadState)
			}
		case plot.StateCancelled, plot.StateExpired:
			delete(proc.schedule, p.ID)
			log.Debug("%s is expired or cancelled", p)
		default:
			return fmt.Errorf("unexpected state (%s)", p.State)
		}
	}

	proc.reporter.render(proc.plots)

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

	for _, plot := range plots {
		proc.schedule[plot.ID] = time.Now()
	}

	log.Infof("%s has %d plots", order, len(plots))

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

func NewProcessor(c *client.Client, r *Reporter, plotDir string, frequency time.Duration) (*Processor, error) {
	p := &Processor{
		client:    c,
		reporter:  r,
		downloads: sync.WaitGroup{},
		frequency: frequency,
		plotDir:   plotDir,
		schedule:  map[string]time.Time{},
	}
	return p, nil
}
