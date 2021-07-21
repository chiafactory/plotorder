package client

import (
	"bytes"
	"chiafactory/plotorder/plot"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cenkalti/backoff/v4"
	log "github.com/sirupsen/logrus"
)

var (
	ErrOrderDoesNotExist  = errors.New("order does not exist")
	ErrPlotHashesNotReady = errors.New("plot hashes not ready")
)

type Client struct {
	apiURL string
	apiKey string
	client *http.Client
}

//GetPlot gets the plot with the given ID
func (c *Client) GetPlot(ctx context.Context, ID string) (*plot.Plot, error) {
	response, err := c.apiRequest(ctx, http.MethodGet, fmt.Sprintf("plots/%s", ID), nil, retryNonOk)
	if err != nil {
		return nil, err
	}

	var r plotResponse
	err = json.Unmarshal(response, &r)
	if err != nil {
		return nil, err
	}

	return &plot.Plot{
		ID:               r.ID,
		State:            plot.State(r.State),
		DownloadURL:      r.URL,
		PlottingProgress: r.Progress,
	}, nil
}

func (c *Client) DeletePlot(ctx context.Context, ID string) (*plot.Plot, error) {
	req := updatePlotRequest{
		ID:    ID,
		State: string(plot.StateExpired),
		//TODO: this is 'downloaded' state. Is it needed?
		DownloadState: 2,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	response, err := c.apiRequest(ctx, http.MethodPut, fmt.Sprintf("plots/%s/", ID), reqBytes, retryNonOk)
	if err != nil {
		return nil, err
	}

	var r plotResponse
	err = json.Unmarshal(response, &r)
	if err != nil {
		return nil, err
	}

	return &plot.Plot{
		ID:               r.ID,
		State:            plot.State(r.State),
		DownloadURL:      r.URL,
		PlottingProgress: r.Progress,
	}, nil
}

func (c *Client) GetHashesForPlot(ctx context.Context, plotID string) ([]string, error) {
	response, err := c.apiRequest(ctx, http.MethodGet, fmt.Sprintf("plots/%s/hashes/", plotID), nil, func(code int) bool {
		// if the hashes are not ready, we'll get a 400, so instead of retrying here we'll
		// let the caller handle it
		if code == http.StatusBadRequest || code == http.StatusOK {
			return false
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	if len(response) <= 0 {
		return nil, ErrPlotHashesNotReady
	}

	r := []string{}
	err = json.Unmarshal(response, &r)
	if err != nil {
		return nil, err
	}

	if len(r) < 1 {
		return nil, ErrPlotHashesNotReady
	}

	return r, nil
}

//GetPlotsForOrderID all the plots for the order with given orderID
func (c *Client) GetPlotsForOrderID(ctx context.Context, orderID string) ([]*plot.Plot, error) {
	response, err := c.apiRequest(ctx, http.MethodGet, fmt.Sprintf("plot_orders/%s", orderID), nil, retryNonOk)
	if err != nil {
		return nil, err
	}

	var r getPlotsForOrderIDResponse
	err = json.Unmarshal(response, &r)
	if err != nil {
		return nil, err
	}

	plots := []*plot.Plot{}
	for _, plotRes := range r.Plots {
		plots = append(plots, &plot.Plot{
			ID:               plotRes.ID,
			State:            plot.State(plotRes.State),
			DownloadURL:      plotRes.URL,
			PlottingProgress: plotRes.Progress,
		})
	}

	return plots, nil
}

type retryFunction func(code int) bool

func retryNonOk(code int) bool {
	return code != http.StatusOK
}

func (c *Client) apiRequest(ctx context.Context, method string, endpoint string, body []byte, retryFunc retryFunction) ([]byte, error) {

	var requestBody io.Reader
	if body != nil {
		requestBody = bytes.NewReader(body)
	}

	url := fmt.Sprintf("%s/%s", c.apiURL, endpoint)
	log.Debugf("%s making %s request to %s", c, method, url)

	req, err := http.NewRequestWithContext(
		ctx,
		method,
		url,
		requestBody,
	)

	header := req.Header
	header.Set("Accept", "application/json")
	header.Set("Content-Type", "application/json")
	header.Set("Authorization", fmt.Sprintf("Token %s", c.apiKey))

	if err != nil {
		return nil, err
	}

	// We'll retry API requests using an exponential back-off schedule
	exp := backoff.NewExponentialBackOff()
	exp.MaxElapsedTime = 1 * time.Minute

	ticker := backoff.NewTicker(exp)
	defer ticker.Stop()

	var responseBody []byte
	for range ticker.C {
		var res *http.Response
		res, err = c.client.Do(req)
		if err != nil {
			log.Errorf("%s error while making %s request to %s: %s", c, method, url, err.Error())
			continue
		}

		responseBody, err = func() ([]byte, error) {
			defer res.Body.Close()
			return io.ReadAll(res.Body)
		}()
		if err != nil {
			log.Errorf("%s error while reading the response body after %s request to %s: %s", c, method, url, err.Error())
			continue
		}

		// Check if we need to retry this API call, based on the provided retryFunc
		retry := retryFunc(res.StatusCode)

		log.Debugf("%s got status code %d for (%s %s, retry=%t)", c, res.StatusCode, method, url, retry)

		// If we don't need to retry, stop the ticker and bail
		if !retry {
			ticker.Stop()
			break
		}
	}

	if err != nil {
		return nil, err
	}

	return responseBody, nil
}

func (c *Client) String() string {
	return "[client]"
}

func NewClient(apiKey, apiURL string) *Client {
	return &Client{
		apiKey: apiKey,
		apiURL: apiURL,
		client: http.DefaultClient,
	}
}
