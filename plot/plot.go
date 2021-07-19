package plot

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
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
	DownloadStateLookingForDownloadDirectory DownloadState = "LOOKING_FOR_DOWNLOAD_DIRECTORY"
	DownloadStateNotStarted                  DownloadState = "NOT_STARTED"
	DownloadStateWaitingForHashes            DownloadState = "WAITING_FOR_HASHES"
	DownloadStatePreparing                   DownloadState = "PREPARING"
	DownloadStateReady                       DownloadState = "READY"
	DownloadStateDownloading                 DownloadState = "DOWNLOADING"
	DownloadStateDownloaded                  DownloadState = "DOWNLOADED"
	DownloadStateFailed                      DownloadState = "FAILED"
	DownloadStateInitialValidation           DownloadState = "INITIAL_VALIDATION"
	DownloadStateLiveValidation              DownloadState = "LIVE_VALIDATION"
	DownloadStateFailedValidation            DownloadState = "FAILED_VALIDATION"
	DownloadStateEnqueued                    DownloadState = "ENQUEUED"
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

	// DownloadURL is the URL we'll download the plot from. It comes from the API and once set
	// it should not be modified
	DownloadURL string

	// fileChunkHashes is a list of hashes that we use to validate the data we download. These come from
	// the API and they **must** be calculated every `hashChunkSize` bytes (the last chunk is the only
	// one that can be smaller)
	fileChunkHashes []string

	downloadState     DownloadState
	downloadDirectory string
	downloadFilename  string
	downloadSize      int64
	downloadHistory   []downloadHistoryRecord
	cancelDownload    context.CancelFunc
	downloadError     bool

	// f is the handle to the file we're downloading (can be nil)
	f *os.File

	// when validation fails, this will be set to the position from which we have to restart downloading
	truncateFrom *int64
}

func (p *Plot) recordDownloadedBytes() {
	if len(p.downloadHistory) >= 5 {
		copy(p.downloadHistory[:], p.downloadHistory[1:])
		p.downloadHistory[len(p.downloadHistory)-1] = downloadHistoryRecord{}
		p.downloadHistory = p.downloadHistory[:len(p.downloadHistory)-1]
	}
	p.downloadHistory = append(p.downloadHistory, downloadHistoryRecord{bytes: p.getDownloadedBytes(), time: time.Now()})
}

func (p *Plot) getDownloadedBytes() int64 {
	if p.f == nil {
		return 0
	}
	fInfo, _ := p.f.Stat()
	return fInfo.Size()
}

func (p *Plot) getLocalFilename() (string, error) {
	parsed, err := url.Parse(p.DownloadURL)
	if err != nil {
		return "", err
	}
	return path.Base(parsed.Path), nil
}

func (p *Plot) updateDownloadState(state DownloadState) {
	if p.downloadState == state {
		return
	}

	prevState := p.downloadState
	p.downloadState = state
	log.Infof("%s download state moved from %s to %s", p, prevState, state)
}

func (p *Plot) GetDownloadState() DownloadState {
	return p.downloadState
}

func (p *Plot) GetDownloadSize() int64 {
	return p.downloadSize
}

func (p *Plot) GetDownloadFilename() string {
	return p.downloadFilename
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
		return "Starting"
	}
	first := p.downloadHistory[0]
	last := p.downloadHistory[len(p.downloadHistory)-1]
	bytesPerSecond := float64(last.bytes-first.bytes) / float64(last.time.Unix()-first.time.Unix())

	// this will happen after a failed chunk validation
	if bytesPerSecond < 0 {
		return "-"
	}

	return fmt.Sprintf("%s/s", humanize.Bytes(uint64(bytesPerSecond)))
}

func (p *Plot) GetDownloadProgress() string {
	if p.State != StatePublished {
		return "-"
	}

	if p.downloadSize == 0 {
		return "-"
	}

	if len(p.downloadHistory) < 1 {
		return "Starting"
	}

	last := p.downloadHistory[len(p.downloadHistory)-1]
	return fmt.Sprintf("%.2f%%", float32(100.0*float64(last.bytes)/float64(p.downloadSize)))
}

func (p *Plot) GetPlottingProgress() string {
	if p.State != StatePlotting {
		return "-"
	}
	return fmt.Sprintf("%.2f%%", float32(p.PlottingProgress))
}

// validateChunk will make sure that the chunk identified by the given `number` is valid.
// This is done by calculating a hash of the chunk and comparing it against the right hash in
// the plot (`p.FileChunkHashes`). Chunks are 0-indexed.
func (p *Plot) validateChunk(number int64) (valid bool, err error) {
	if int(number) >= len(p.fileChunkHashes) {
		return false, fmt.Errorf("chunk to verify (%d; 0-indexed) is greater than the available number of hashes (%d)", number, len(p.fileChunkHashes))
	}

	// always align to a multiple of `hashChunkSize` bytes
	stop := (number + 1) * hashChunkSize
	start := stop - hashChunkSize

	// and now adjust `stop` to limit it to the total plot file size
	if stop > p.downloadSize {
		stop = p.downloadSize
	}

	// use different handle so we can seek safely (the download might still be in progress)
	handle, err := os.Open(p.f.Name())
	if err != nil {
		return false, err
	}
	defer handle.Close()

	log.Infof("%s is validating chunk %d (%d -> %d)", p, number, start, stop)

	// align to start of chunk
	_, err = handle.Seek(start, io.SeekStart)
	if err != nil {
		return
	}

	// `io.LimitReader` will stop when it reads `hashChunkSize` bytes OR when the underlying reader
	// is exhausted, whatever happens first
	var chunkHash string
	chunkHash, err = calculateChunkHash(io.LimitReader(handle, hashChunkSize))
	if err != nil {
		log.Errorf("there was an error calculating the hash for one of the plot chunks (%s)", err.Error())
		return
	}

	// chunks are 0-indexed
	expectedChunkHash := p.fileChunkHashes[number]
	if chunkHash == expectedChunkHash {
		valid = true
		log.Infof("%s chunk is valid (calculated=%s, expected=%s)", p, chunkHash, expectedChunkHash)
	} else {
		log.Errorf("%s chunk is invalid (calculated=%s, expected=%s). We'll resume downlaoding from %d", p, chunkHash, expectedChunkHash, start)
		p.truncateFrom = &start
	}

	return valid, nil
}

func (p *Plot) getDownloadFilename() (filepath string, err error) {
	parsed, err := url.Parse(p.DownloadURL)
	if err != nil {
		return "", err
	}
	return path.Base(parsed.Path), nil
}

func (p *Plot) getDownloadSize() (fileSize int64, err error) {
	req, err := http.NewRequest(http.MethodHead, p.DownloadURL, nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		err = errors.Wrap(err, "error while making the HTTP request to download the file")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("invalid status code returned (%d) while trying to get download size", resp.StatusCode)
		return
	}

	contentLength := resp.Header.Get("Content-Length")
	return strconv.ParseInt(contentLength, 10, 0)
}

func (p *Plot) startValidator(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		prevChunkN := p.getDownloadedBytes() / hashChunkSize
		defer ticker.Stop()

		for range ticker.C {
			downloaded := p.getDownloadedBytes()
			currChunkN := downloaded / hashChunkSize
			if currChunkN != prevChunkN || downloaded == p.downloadSize {
				prevState := p.downloadState
				p.updateDownloadState(DownloadStateLiveValidation)

				// handle last chunk
				chunk := prevChunkN
				if downloaded == p.downloadSize {
					chunk = currChunkN
				}

				var valid bool
				valid, err := p.validateChunk(chunk)
				if err != nil {
					log.Errorf("%s error while validating chunk (%d): %s", p, chunk, err.Error())
					p.updateDownloadState(DownloadStateFailedValidation)
					return
				}

				if !valid {
					p.updateDownloadState(DownloadStateFailedValidation)
					return
				}
				prevChunkN = currChunkN
				p.updateDownloadState(prevState)
			}

			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()
}

func (p *Plot) startRecorder(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			p.recordDownloadedBytes()

			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()
}

func (p *Plot) GetRemainingBytes() int64 {
	return p.downloadSize - p.getDownloadedBytes()
}

func (p *Plot) SetDownloadError() {
	p.downloadError = true
}

func (p *Plot) HasDownloadError() bool {
	return p.downloadError
}

func (p *Plot) InitialiseDownload() error {
	fileSize, err := p.getDownloadSize()
	if err != nil {
		return err
	}
	p.downloadSize = fileSize

	fileName, err := p.getDownloadFilename()
	if err != nil {
		return err
	}
	p.downloadFilename = fileName
	p.updateDownloadState(DownloadStateLookingForDownloadDirectory)
	return nil
}

func (p *Plot) requiredNumberOfFileHashes() int {
	return int(math.Ceil(float64(p.downloadSize) / float64(hashChunkSize)))
}

func (p *Plot) SetFileHashes(hashes []string) {
	required := p.requiredNumberOfFileHashes()
	if len(hashes) < required {
		log.Warnf("%s does not yet have all required plot file verification hashes (has=%d, requires=%s)", p, len(hashes), required)
		return
	}
	p.fileChunkHashes = hashes
	log.Debugf("%s using %d plot file verification hashes", p, len(hashes))
	p.updateDownloadState(DownloadStateNotStarted)
}

func (p *Plot) SetDownloadDirectory(dir string) (err error) {
	filePath := path.Join(dir, p.downloadFilename)

	// we'll create a new file if it does not exist or append to
	// it if it does
	var openFlags int
	_, err = os.Stat(filePath)
	if err == nil {
		openFlags = os.O_RDWR | os.O_APPEND
	} else {
		openFlags = os.O_CREATE | os.O_EXCL | os.O_RDWR
	}

	// get file handle that we'll use for the download
	var file *os.File
	file, err = os.OpenFile(filePath, openFlags, os.ModePerm)
	if err != nil {
		err = errors.Wrap(err, "could not open the file for writing")
		return
	}

	p.f = file
	p.downloadDirectory = dir

	p.updateDownloadState(DownloadStateWaitingForHashes)
	return nil
}

func (p *Plot) GetDownloadDirectory() string {
	return p.downloadDirectory
}

func (p *Plot) SetDownloadEnqueued() {
	p.updateDownloadState(DownloadStateEnqueued)
}

func (p *Plot) PrepareDownload(ctx context.Context) (err error) {
	defer func() {
		if err != nil {
			p.updateDownloadState(DownloadStateFailed)
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

	// as soon as we get into here, we'll mark this as false
	p.downloadError = false

	fInfo, _ := p.f.Stat()
	downloaded := fInfo.Size()

	// if the file has been fully downloaded, stop here
	if downloaded == p.downloadSize {
		log.Infof("%s is already downloaded", p)
		p.updateDownloadState(DownloadStateDownloaded)
		return
	}

	// nothing else to do if the file is empty
	if downloaded == 0 {
		p.updateDownloadState(DownloadStateReady)
		return
	}

	// or if we have not downloaded any full chunk yet
	if downloaded < hashChunkSize {
		p.updateDownloadState(DownloadStateReady)
		return
	}

	// otherwise, find the last **full** chunk
	chunkNumber := (downloaded / hashChunkSize) - 1

	p.updateDownloadState(DownloadStateInitialValidation)

	// and make sure it's valid
	valid, err := p.validateChunk(chunkNumber)
	if err != nil {
		log.Errorf("there was an error while trying to load the last chunk for validation (%s)", err.Error())
		return
	}
	if !valid {
		p.updateDownloadState(DownloadStateFailedValidation)
		return
	}

	p.updateDownloadState(DownloadStateReady)
	return
}

func (p *Plot) RetryDownload(ctx context.Context) (err error) {
	// if there's an active download, cancel it
	if p.cancelDownload != nil {
		log.Infof("%s cancelling current download", p)
		p.cancelDownload()
	}

	log.Infof("%s retrying download", p)
	p.updateDownloadState(DownloadStateReady)
	return nil
}

func (p *Plot) Download(ctx context.Context) (err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		p.cancelDownload = nil
	}()

	p.cancelDownload = cancel

	defer func() {
		if err != nil {
			log.Errorf("%s download failed: %s", p, err.Error())
			p.updateDownloadState(DownloadStateFailed)
		} else if p.getDownloadedBytes() == p.downloadSize {
			log.Infof("%s download finished", p)
			p.updateDownloadState(DownloadStateDownloaded)
		} else {
			log.Infof("%s download was aborted (%s downloaded)", p, humanize.Bytes(uint64(p.getDownloadedBytes())))
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

	// if this is set, it means validation failed, so we have to truncate the file and resume from there
	if p.truncateFrom != nil {
		err = p.f.Truncate(*p.truncateFrom)
		if err != nil {
			return
		}

		_, err = p.f.Seek(0, io.SeekEnd)
		if err != nil {
			return
		}
		p.truncateFrom = nil
	}

	p.updateDownloadState(DownloadStateDownloading)

	var req *http.Request
	req, err = http.NewRequest(http.MethodGet, p.DownloadURL, nil)
	if err != nil {
		return
	}

	var (
		expectedStatusCode = http.StatusOK
		downloadedBytes    = p.getDownloadedBytes()
	)
	if downloadedBytes > 0 {
		expectedStatusCode = http.StatusPartialContent
		log.Infof("%s resuming download (%s already downloaded) from %s into %s", p, humanize.Bytes(uint64(downloadedBytes)), p.DownloadURL, p.f.Name())
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", downloadedBytes))
	} else {
		log.Infof("%s starting download from %s into %s", p, p.DownloadURL, p.f.Name())
	}

	var resp *http.Response
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		err = errors.Wrap(err, "error while making the HTTP request to download the file")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != expectedStatusCode {
		err = fmt.Errorf("invalid status code returned (%d)", resp.StatusCode)
		return
	}

	var chunkSize = int64(8192)

	// when this channel gets written into, we'll finish the download process
	done := make(chan error)
	p.startValidator(ctx)
	p.startRecorder(ctx)
	go func() {

		var (
			chunk    = make([]byte, chunkSize)
			filebuff = bufio.NewWriterSize(p.f, int(chunkSize))
			err      error
		)

		defer func() {
			filebuff.Flush()
			done <- err
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
				filebuff.Write(chunk[0:r])
			}

			if readErr == io.EOF {
				break
			}

			if readErr != nil {
				err = readErr
				log.Errorf("there was an error reading the plot file from the server (%s)", err.Error())
				return
			}
		}
	}()

	err = <-done
	return err
}

func (p *Plot) String() string {
	return fmt.Sprintf("[plot id=%s]", p.ID)
}
