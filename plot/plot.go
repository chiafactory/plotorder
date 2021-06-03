package plot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type State string

const (
	// Plot statuses (from the API)
	StatePending   State = "P"
	StatePlotting  State = "R"
	StatePublished State = "D"
	StateCancelled State = "C"
	StateExpired   State = "X"
)

type DownloadState string

const (
	// Plot download statuses (only used in this tool)
	DownloadStateNotStarted  DownloadState = "NOT_STARTED"
	DownloadStateDownloading DownloadState = "DOWNLOADING"
	DownloadStateDownloaded  DownloadState = "DOWNLOADED"
	DownloadStateFailed      DownloadState = "FAILED"
)

type downloadHistoryRecord struct {
	bytes int
	time  time.Time
}

type Plot struct {
	// ID is the unique identifier for this plot, as set by the API
	ID string

	// State comes from the API and tells us about the lifecycle of the plot
	State State

	// PlottingProgress (0-100%) is obtained from the API and tells us about the plotting process
	// for this plot
	PlottingProgress int

	// DownloadURL is the URL to download the plot file
	DownloadURL string

	// DownloadState tells us if the plot is currently being downloaded or the download
	// failed for any reason. When the plot is initialised, this is set to `DownloadStateNotStarted`
	// and it gets updated as soon as the download starts
	DownloadState DownloadState

	downloadHistory   []downloadHistoryRecord
	downloadLocalPath string
	downloadSize      int
}

func (p *Plot) UpdateDownloadState(state DownloadState) {
	prevState := p.DownloadState
	p.DownloadState = state
	log.Infof("%s download state moved from %s to %s", p, prevState, state)
}

func (p *Plot) UpdateState(state State) {
	prevState := p.State
	p.State = state
	log.Infof("%s state moved from %s to %s", p, prevState, state)
}

func (p *Plot) UpdatePlottingProgress(progress int) {
	p.PlottingProgress = progress
}

func (p *Plot) recordDownloadedBytes(bytes int) {
	if len(p.downloadHistory) > 5 {
		dst := []downloadHistoryRecord{}
		copy(dst, p.downloadHistory)
		dst = append(dst, downloadHistoryRecord{bytes: bytes, time: time.Now()})
		p.downloadHistory = dst
	} else {
		p.downloadHistory = append(p.downloadHistory, downloadHistoryRecord{bytes: bytes, time: time.Now()})
	}
}

func (p *Plot) getLocalFilename() (string, error) {
	parsed, err := url.Parse(p.DownloadURL)
	if err != nil {
		return "", err
	}
	return path.Base(parsed.Path), nil
}

func (p *Plot) GetDownloadSpeed() uint64 {
	if len(p.downloadHistory) < 2 {
		return 0
	}
	first := p.downloadHistory[0]
	last := p.downloadHistory[len(p.downloadHistory)-1]
	return uint64(float64((first.bytes - last.bytes)) / float64((int(first.time.Unix()) - int(last.time.Unix()))))
}

func (p *Plot) GetDownloadProgress() float32 {
	if len(p.downloadHistory) < 1 {
		return 0
	}

	if p.downloadSize == 0 {
		return 0
	}

	last := p.downloadHistory[len(p.downloadHistory)-1]
	return float32(100.0 * float64(last.bytes) / float64(p.downloadSize))
}

func (p *Plot) GetDownloadLocalPath() string {
	return p.downloadLocalPath
}

func (p *Plot) Download(ctx context.Context, plotDir string) (err error) {
	var (
		finished bool
	)

	defer func() {
		if err != nil {
			log.Errorf("%s download failed: %s", p, err.Error())
			p.UpdateDownloadState(DownloadStateFailed)
			return
		} else if finished {
			log.Errorf("%s download finished", p)
			p.UpdateDownloadState(DownloadStateDownloaded)
		} else {
			log.Infof("%s download was aborted", p)
		}
	}()

	defer func() {
		if r := recover(); r != nil {
			switch val := r.(type) {
			case string:
				err = errors.New(val)
			case error:
				err = val
			default:
				err = errors.New("unhandled error ocurred")
			}
		}
	}()

	fileName, err := p.getLocalFilename()
	if err != nil {
		return
	}

	filePath := path.Join(plotDir, fileName)
	p.downloadLocalPath = filePath

	p.UpdateDownloadState(DownloadStateDownloading)

	// we'll create a new file if it does not exist or append to
	// it if it does
	var openFlags int
	_, err = os.Stat(filePath)
	if err == nil {
		openFlags = os.O_APPEND | os.O_RDWR
	} else {
		openFlags = os.O_CREATE | os.O_EXCL | os.O_RDWR
	}

	// open the file
	file, err := os.OpenFile(filePath, openFlags, os.ModePerm)
	if err != nil {
		err = errors.Wrap(err, "could not open the file for writing")
		return
	}

	// check if we're resuming a download. If we are, we'll start
	// consuming from the right position
	downloaded, err := file.Seek(0, os.SEEK_END)
	if err != nil {
		err = errors.Wrap(err, "could not seek the file")
		return
	}

	req, err := http.NewRequest(http.MethodGet, p.DownloadURL, nil)
	if err != nil {
		return
	}

	expectedStatusCode := http.StatusOK
	if downloaded > 0 {
		expectedStatusCode = http.StatusPartialContent
		log.Infof("%s resuming download (%s already downloaed) from %s into %s", p, humanize.Bytes(uint64(downloaded)), p.DownloadURL, filePath)
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", downloaded))
	} else {
		log.Infof("%s starting download from %s into %s", p, p.DownloadURL, filePath)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		err = errors.Wrap(err, "error while making the HTTP request to download the file")
		return
	}

	if resp.StatusCode != expectedStatusCode {
		err = fmt.Errorf("invalid status code returned (%d)", resp.StatusCode)
		return
	}

	// figure out the total size of the plot file
	totalSize := downloaded
	contentLength := resp.Header.Get("Content-Length")
	if contentLength != "" {
		var remainingBytes int64
		remainingBytes, err = strconv.ParseInt(contentLength, 10, 0)
		if err != nil {
			return
		}
		totalSize += remainingBytes
	} else {
		err = errors.New("unable to get the plot file size. Aborting")
		return
	}

	p.downloadSize = int(totalSize)

	chunkSize := int64(8192)
	go func() {
		defer func() {
			resp.Body.Close()
		}()

		b := make([]byte, chunkSize)
		for {

			// if the context has been cancelled, bail here
			select {
			case <-ctx.Done():
				return
			default:
			}

			// otherwise, read a new chunk and write it to our file
			r, err := resp.Body.Read(b)
			if err == io.EOF {
				finished = true
				break
			}
			downloaded += int64(r)
			file.Write(b[0:r])
		}
	}()

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				p.recordDownloadedBytes(int(downloaded))
			}
		}
	}()

	<-ctx.Done()
	return nil
}

func (p *Plot) String() string {
	return fmt.Sprintf("Plot [id=%s]", p.ID)
}
