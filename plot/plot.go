package plot

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/base64"
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
	DownloadStateDownloading     DownloadState = "DOWNLOADING"
	DownloadStateDownloaded      DownloadState = "DOWNLOADED"
	DownloadStateFailed          DownloadState = "FAILED"
	DownloadStateVadidatingChunk DownloadState = "VALIDATING_CHUNK"
)

// validateChunkSize is the maximum size (in bytes) of the chunks we'll validate
const validateChunkSize = int64(1000 * 1000 * 1000)

type downloadHistoryRecord struct {
	bytes int64
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

	resumeFromBytes   *int64
	downloadHistory   []downloadHistoryRecord
	downloadLocalPath string
	downloadSize      int64
	downloadedBytes   int64
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

func (p *Plot) recordDownloadedBytes(bytes int64) {
	if len(p.downloadHistory) >= 5 {
		copy(p.downloadHistory[:], p.downloadHistory[1:])
		p.downloadHistory[len(p.downloadHistory)-1] = downloadHistoryRecord{}
		p.downloadHistory = p.downloadHistory[:len(p.downloadHistory)-1]
	}
	p.downloadHistory = append(p.downloadHistory, downloadHistoryRecord{bytes: bytes, time: time.Now()})
	p.downloadedBytes = bytes
}

func (p *Plot) getLocalFilename() (string, error) {
	parsed, err := url.Parse(p.DownloadURL)
	if err != nil {
		return "", err
	}
	return path.Base(parsed.Path), nil
}

func (p *Plot) GetDownloadSpeed() int64 {
	if len(p.downloadHistory) < 2 {
		return 0
	}
	first := p.downloadHistory[0]
	last := p.downloadHistory[len(p.downloadHistory)-1]
	bytesPerSecond := float64(last.bytes-first.bytes) / float64(last.time.Unix()-first.time.Unix())

	// this will happen after a failed chunk validation
	if bytesPerSecond < 0 {
		return 0
	}

	logrus.Debugf("%s comparing %d vs %d, which took %d seconds -> %s", p, last.bytes, first.bytes, (int(last.time.Unix()) - int(first.time.Unix())), humanize.Bytes(uint64(bytesPerSecond)))
	return int64(bytesPerSecond)
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

func (p *Plot) calculateChunkHash(chunk io.Reader) (string, error) {
	h := md5.New()
	buffer := make([]byte, 100*1000*1000)
	for {
		r, err := chunk.Read(buffer)
		if r > 0 {
			h.Write(buffer[:r])
		}

		if err == io.EOF {
			break
		}

		if err != nil {
			return "", err
		}
	}

	b64Hash := base64.StdEncoding.EncodeToString(h.Sum(nil))
	return b64Hash, nil
}

func (p *Plot) Download(ctx context.Context, plotDir string, b64ChunkHashes []string) (err error) {
	var (
		finished           bool
		needsRedownloading bool
	)

	defer func() {
		if err != nil {
			log.Errorf("%s download failed: %s", p, err.Error())
			p.UpdateDownloadState(DownloadStateFailed)
		} else if finished {
			log.Errorf("%s download finished", p)
			p.UpdateDownloadState(DownloadStateDownloaded)
		} else if needsRedownloading {
			log.Errorf("%s needs re-downloading (last chunk only)", p)
			p.UpdateDownloadState(DownloadStateNotStarted)
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

	// if `resumeFromBytes` is set, we'll need to start writing to the file
	// right after that. If it's not set, we'll find out what's the last
	// position in the file and start from there
	var (
		downloaded               int64
		bytesSinceLastChunkCheck int64
	)
	if p.resumeFromBytes != nil {
		downloaded, err = file.Seek(*p.resumeFromBytes, io.SeekStart)
		p.resumeFromBytes = nil
		bytesSinceLastChunkCheck = 0
	} else {
		downloaded, err = file.Seek(0, io.SeekEnd)
		bytesSinceLastChunkCheck = downloaded
	}
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
	totalSize := int64(downloaded)
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

	p.downloadSize = totalSize

	chunkSize := int64(8192)
	done := make(chan struct{})
	go func() {
		chunk := make([]byte, chunkSize)

		// this will make sure we exactly write `chunkSize` bytes to disk every
		// time
		filebuff := bufio.NewWriterSize(file, int(chunkSize))

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
			r, err := resp.Body.Read(chunk)
			if r > 0 {
				downloaded += int64(r)
				bytesSinceLastChunkCheck += int64(r)
				filebuff.Write(chunk[0:r])
			}

			// validate if we just processed a chunk of `validateChunkSize` bytes OR
			// if we're done downloading (to make sure we also validate the last chunk)
			if bytesSinceLastChunkCheck >= validateChunkSize || err == io.EOF {
				// upload download state while we're validating
				p.UpdateDownloadState(DownloadStateVadidatingChunk)

				chunkLimit := validateChunkSize
				if bytesSinceLastChunkCheck < validateChunkSize {
					chunkLimit = bytesSinceLastChunkCheck
				}

				bytesSinceLastChunkCheck = 0

				chunkN := (downloaded / validateChunkSize) - 1
				startPos := chunkN * validateChunkSize
				logrus.Infof("%s is validating chunk %d (%d -> %d)", p, chunkN, startPos, chunkLimit)

				// align with the start of the chunk
				file.Seek(startPos, io.SeekStart)
				b64Hash, err := p.calculateChunkHash(io.LimitReader(file, chunkLimit))
				if err != nil {
					logrus.Errorf("there was an error calculating the hash for one of the plot chunks (%s)", err.Error())
					return
				}

				// compare the hash we calculated with the one from the API
				isChunkValid := false
				for idx := range b64ChunkHashes {
					if int64(idx) == chunkN-1 && b64ChunkHashes[idx] == b64Hash {
						isChunkValid = true
						break
					}
				}

				// reset download state
				p.UpdateDownloadState(DownloadStateDownloading)

				// if the chunk is not valid, we'll need to download it again
				if !isChunkValid {
					logrus.Errorf("%s the last downloaded chunk was corrupted. It'll be downloaded again", p)
					p.resumeFromBytes = &startPos
					needsRedownloading = true
					return
				}

				// otherwise, continue with download
				file.Seek(0, io.SeekEnd)
			}

			if err == io.EOF {
				finished = true
				break
			}

			if err != nil {
				logrus.Errorf("there was an error reading the plot file from the server (%s)", err.Error())
				return
			}
		}
	}()

	ticker := time.NewTicker(1 * time.Second)
	for {
		select {
		case <-done:
			ticker.Stop()
			return
		case <-ctx.Done():
			ticker.Stop()
			return
		case <-ticker.C:
			p.recordDownloadedBytes(int64(downloaded))
		}
	}
}

func (p *Plot) String() string {
	return fmt.Sprintf("Plot [id=%s]", p.ID)
}
