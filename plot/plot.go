package plot

import (
	"bufio"
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
	"github.com/sirupsen/logrus"
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
	DownloadStateNotStarted      DownloadState = "NOT_STARTED"
	DownloadStatePreparing       DownloadState = "PREPARING"
	DownloadStateReady           DownloadState = "READY"
	DownloadStateDownloading     DownloadState = "DOWNLOADING"
	DownloadStateDownloaded      DownloadState = "DOWNLOADED"
	DownloadStateFailed          DownloadState = "FAILED"
	DownloadStateValidatingChunk DownloadState = "VALIDATING_CHUNK"
)

// hashChunkSize is the maximum size (in bytes) of the chunks we'll validate
const hashChunkSize = int64(10 * 1000 * 1000 * 1000)

type downloadHistoryRecord struct {
	bytes int64
	time  time.Time
}

type Plot struct {
	// ID is the unique identifier for this plot, as set by the API
	ID string

	// State comes from the API and tells us about the lifecycle of the plot
	State State

	// PlottingProgress (0-100) is obtained from the API and tells us about the plotting process
	// for this plot
	PlottingProgress int

	// DownloadURL is the URL to download the plot file
	DownloadURL string

	// DownloadState tells us if the plot is currently being downloaded or the download
	// failed for any reason. When the plot is initialised, this is set to `DownloadStateNotStarted`
	// and it gets updated as soon as the download starts
	DownloadState DownloadState

	// FileHashes is a list of hashes we can use to validate the chunks we download. These come from
	// the API and they **must** be calculated every `hashChunkSize` bytes (the last chunk is the only
	// one that can be smaller)
	FileHashes []string

	downloadHistory []downloadHistoryRecord
	downloadSize    int64
	downloadedBytes int64

	// f is the handle to the file we're downloading (can be nil)
	f *os.File
}

func (p *Plot) recordDownloadedBytes() {
	if len(p.downloadHistory) >= 5 {
		copy(p.downloadHistory[:], p.downloadHistory[1:])
		p.downloadHistory[len(p.downloadHistory)-1] = downloadHistoryRecord{}
		p.downloadHistory = p.downloadHistory[:len(p.downloadHistory)-1]
	}
	p.downloadHistory = append(p.downloadHistory, downloadHistoryRecord{bytes: p.downloadedBytes, time: time.Now()})
}

func (p *Plot) getLocalFilename() (string, error) {
	parsed, err := url.Parse(p.DownloadURL)
	if err != nil {
		return "", err
	}
	return path.Base(parsed.Path), nil
}

func (p *Plot) updateDownloadState(state DownloadState) {
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

func (p *Plot) GetDownloadSpeed() string {
	if len(p.downloadHistory) < 2 {
		return "N/A"
	}
	first := p.downloadHistory[0]
	last := p.downloadHistory[len(p.downloadHistory)-1]
	bytesPerSecond := float64(last.bytes-first.bytes) / float64(last.time.Unix()-first.time.Unix())

	// this will happen after a failed chunk validation
	if bytesPerSecond < 0 {
		return "N/A"
	}

	return fmt.Sprintf("%s/s", humanize.Bytes(uint64(bytesPerSecond)))
}

func (p *Plot) GetDownloadProgress() string {
	if p.State != StatePublished {
		return "N/A"
	}

	if len(p.downloadHistory) < 1 {
		return "N/A"
	}

	if p.downloadSize == 0 {
		return "N/A"
	}

	last := p.downloadHistory[len(p.downloadHistory)-1]
	return fmt.Sprintf("%.2f%%", float32(100.0*float64(last.bytes)/float64(p.downloadSize)))
}

func (p *Plot) GetPlottingProgress() string {
	if p.State != StatePlotting {
		return "N/A"
	}
	return fmt.Sprintf("%d%%", p.PlottingProgress)
}

func (p *Plot) validateChunk(number int64) (valid bool, start int64, err error) {
	// always align to a multiple of `hashChunkSize` bytes
	stop := (number + 1) * hashChunkSize
	start = stop - hashChunkSize

	// and now adjust `stop` to limit it to the total plot file size
	if stop > p.downloadSize {
		stop = p.downloadSize
	}

	logrus.Infof("%s is validating chunk %d (%d -> %d)", p, number, start, stop)

	// align to start of chunk
	_, err = p.f.Seek(start, io.SeekStart)
	if err != nil {
		return
	}

	// `io.LimitReader` will stop when it reads `hashChunkSize` bytes OR when the underlying reader
	// is exhausted, whatever happens first
	var chunkHash string
	chunkHash, err = calculateChunkHash(io.LimitReader(p.f, hashChunkSize))
	if err != nil {
		logrus.Errorf("there was an error calculating the hash for one of the plot chunks (%s)", err.Error())
		return
	}

	// chunks are 0-indexed
	for idx := range p.FileHashes {
		if idx == int(number) {
			if p.FileHashes[idx] == chunkHash {
				logrus.Infof("%s chunk is valid (calculated=%s, expected=%s)", p, chunkHash, p.FileHashes[idx])
				valid = true
			} else {
				logrus.Errorf("%s chunk is invalid (calculated=%s, expected=%s)", p, chunkHash, p.FileHashes[idx])
			}
			break
		}
	}

	return valid, start, nil
}

func (p *Plot) GetDownloadFilename() (filepath string, err error) {
	parsed, err := url.Parse(p.DownloadURL)
	if err != nil {
		return "", err
	}
	return path.Base(parsed.Path), nil
}

func (p *Plot) GetDownloadSize() (fileSize int64, err error) {
	req, err := http.NewRequest(http.MethodHead, p.DownloadURL, nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		err = errors.Wrap(err, "error while making the HTTP request to download the file")
		return
	}

	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("invalid status code returned (%d) while trying to get download size", resp.StatusCode)
		return
	}

	contentLength := resp.Header.Get("Content-Length")
	fileSize, err = strconv.ParseInt(contentLength, 10, 0)
	if err != nil {
		return
	}
	return fileSize, err
}

func (p *Plot) PrepareDownload(ctx context.Context, plotDir string) (err error) {
	defer func() {
		if err != nil {
			p.updateDownloadState(DownloadStateFailed)
		} else {
			p.updateDownloadState(DownloadStateReady)
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

	p.updateDownloadState(DownloadStatePreparing)

	// figure out the size of the file
	fileSize, err := p.GetDownloadSize()
	if err != nil {
		return
	}
	p.downloadSize = fileSize

	filename, err := p.GetDownloadFilename()
	if err != nil {
		return err
	}
	filePath := path.Join(plotDir, filename)

	// we'll create a new file if it does not exist or append to
	// it if it does
	var openFlags int
	fInfo, err := os.Stat(filePath)
	if err == nil {
		openFlags = os.O_RDWR | os.O_APPEND

		// if the file has been fully downloaded, stop here
		if fInfo.Size() == fileSize {
			p.updateDownloadState(DownloadStateDownloaded)
			return nil
		}
	} else {
		openFlags = os.O_CREATE | os.O_EXCL | os.O_RDWR
	}

	file, err := os.OpenFile(filePath, openFlags, os.ModePerm)
	if err != nil {
		err = errors.Wrap(err, "could not open the file for writing")
		return
	}
	p.f = file

	downloaded, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		err = errors.Wrap(err, "could not seek the file")
		return
	}

	// nothing to do if the file is empty
	if downloaded == 0 {
		return
	}

	// now we'll validate the existing file
	var (
		chunkNumber = int64(0)
		remaining   = downloaded
	)

	// by using this condition this we avoid checking incomplete chunks when
	// we prepare the download (as it's partial content)
	for remaining >= hashChunkSize {
		valid, start, err := p.validateChunk(chunkNumber)
		if err != nil {
			logrus.Errorf("there was an error while trying to load the last chunk for validation (%s)", err.Error())
			return err
		}

		if !valid {
			logrus.Infof("%s truncating to %d", p, start)
			err = p.f.Truncate(start)
			if err != nil {
				return err
			}
			_, err = p.f.Seek(0, io.SeekEnd)
			if err != nil {
				return err
			}
			downloaded = start
			break
		}

		remaining -= hashChunkSize
		chunkNumber++
	}

	p.downloadedBytes = downloaded

	return nil
}

func (p *Plot) Download(ctx context.Context) (err error) {
	var (
		finished           bool
		needsRedownloading bool
	)

	defer func() {
		if err != nil {
			log.Errorf("%s download failed: %s", p, err.Error())
			p.updateDownloadState(DownloadStateFailed)
		} else if finished {
			log.Errorf("%s download finished", p)
			p.updateDownloadState(DownloadStateDownloaded)
		} else if needsRedownloading {
			log.Errorf("%s needs re-downloading (last chunk only)", p)
			p.updateDownloadState(DownloadStateReady)
		} else {
			log.Infof("%s download was aborted (%s downloaded)", p, humanize.Bytes(uint64(p.downloadedBytes)))
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

	p.updateDownloadState(DownloadStateDownloading)

	req, err := http.NewRequest(http.MethodGet, p.DownloadURL, nil)
	if err != nil {
		return
	}

	expectedStatusCode := http.StatusOK
	if p.downloadedBytes > 0 {
		expectedStatusCode = http.StatusPartialContent
		log.Infof("%s resuming download (%s already downloaded) from %s into %s", p, humanize.Bytes(uint64(p.downloadedBytes)), p.DownloadURL, p.f.Name())
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", p.downloadedBytes))
	} else {
		log.Infof("%s starting download from %s into %s", p, p.DownloadURL, p.f.Name())
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

	var (
		chunkSize  = int64(8192)
		done       = make(chan struct{})
		prevChunkN = p.downloadedBytes / hashChunkSize
		currChunkN = prevChunkN
	)

	go func() {
		chunk := make([]byte, chunkSize)

		// make sure we exactly write `chunkSize` bytes to disk every time
		filebuff := bufio.NewWriterSize(p.f, int(chunkSize))

		defer func() {
			resp.Body.Close()
			filebuff.Flush()
			done <- struct{}{}
		}()

		for {

			// if the context has been cancelled, bail here
			select {
			case <-ctx.Done():
				return
			default:
			}

			// otherwise, read a new chunk and write it to our file
			r, readErr := resp.Body.Read(chunk)
			if r > 0 {
				p.downloadedBytes += int64(r)
				currChunkN = p.downloadedBytes / hashChunkSize
				filebuff.Write(chunk[0:r])
			}

			// check hash if we just processed a chunk of `hashChunkSize` bytes OR
			// if we're done downloading (to make sure we also validate the last chunk)
			if currChunkN != prevChunkN || readErr == io.EOF {
				err = filebuff.Flush()
				if err != nil {
					return
				}

				// upload download state while we're validating
				p.updateDownloadState(DownloadStateValidatingChunk)

				valid, start, err := p.validateChunk(prevChunkN)
				if err != nil {
					return
				}

				// update previous chunk number
				prevChunkN = currChunkN

				// if the chunk is not valid, truncate the file (down to the end of the last
				// valid chunk)
				if !valid {
					logrus.Infof("%s truncating to %d", p, start)
					err = p.f.Truncate(start)
					if err != nil {
						return
					}
					_, err = p.f.Seek(0, io.SeekEnd)
					if err != nil {
						return
					}
					p.downloadedBytes = start
					needsRedownloading = true
					return
				}

				// reset download state
				p.updateDownloadState(DownloadStateDownloading)

				// otherwise, continue with download
				p.f.Seek(0, io.SeekEnd)
			}

			if readErr == io.EOF {
				finished = true
				break
			}

			if readErr != nil {
				logrus.Errorf("there was an error reading the plot file from the server (%s)", err.Error())
				return
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				done <- struct{}{}
				return
			case <-ticker.C:
				p.recordDownloadedBytes()
			}
		}
	}()
	<-done
	return nil
}

func (p *Plot) String() string {
	return fmt.Sprintf("Plot [id=%s]", p.ID)
}
