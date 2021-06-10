package client

import (
	"bytes"
	"chiafactory/plotorder/order"
	"chiafactory/plotorder/plot"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/sirupsen/logrus"
)

var (
	ErrOrderDoesNotExist = errors.New("order does not exist")
)

type Client struct {
	apiURL string
	apiKey string
	client *http.Client
}

// GetOrders gets the order for the given ID
func (c *Client) GetOrder(ctx context.Context, ID string) (*order.Order, error) {
	response, err := c.apiRequest(ctx, http.MethodGet, fmt.Sprintf("plot_orders/%s", ID), nil)
	if err != nil {
		return nil, err
	}

	var r orderResponse
	err = json.Unmarshal(response, &r)
	if err != nil {
		return nil, err
	}

	return &order.Order{ID: r.ID}, nil
}

// GetOrders lists all the orders for the given account
func (c *Client) GetOrders(ctx context.Context) ([]*order.Order, error) {
	response, err := c.apiRequest(ctx, http.MethodGet, "plot_orders", nil)
	if err != nil {
		return nil, err
	}

	var r getOrdersResponse
	err = json.Unmarshal(response, &r)
	if err != nil {
		return nil, err
	}

	orders := []*order.Order{}
	for _, result := range r.Results {
		orders = append(orders, &order.Order{ID: result.ID})
	}

	return orders, nil
}

//GetPlot gets the plot with the given ID
func (c *Client) GetPlot(ctx context.Context, ID string) (*plot.Plot, error) {
	response, err := c.apiRequest(ctx, http.MethodGet, fmt.Sprintf("plots/%s", ID), nil)
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
		DownloadState:    plot.DownloadStateNotStarted,
		DownloadURL:      r.URL,
		PlottingProgress: r.Progress,
	}, nil
}

func (c *Client) DeletePlot(ctx context.Context, ID string) error {
	req := updatePlotRequest{
		ID:    ID,
		State: string(plot.StateExpired),
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return err
	}

	response, err := c.apiRequest(ctx, http.MethodPut, fmt.Sprintf("plots/%s", ID), reqBytes)
	if err != nil {
		return err
	}

	var r plotResponse
	return json.Unmarshal(response, &r)
}

func (c *Client) GetHashesForPlot(ctx context.Context, plotID string) ([]string, error) {
	response, err := c.apiRequest(ctx, http.MethodGet, fmt.Sprintf("plots/%s/hashes/", plotID), nil)
	if err != nil {
		return nil, err
	}

	r := []string{}
	err = json.Unmarshal(response, &r)
	if err != nil {
		return nil, err
	}
	return r, nil
}

//GetPlotsForOrderID all the plots for the order with given orderID
func (c *Client) GetPlotsForOrderID(ctx context.Context, orderID string) ([]*plot.Plot, error) {
	response, err := c.apiRequest(ctx, http.MethodGet, fmt.Sprintf("plot_orders/%s", orderID), nil)
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
			DownloadState:    plot.DownloadStateNotStarted,
			DownloadURL:      plotRes.URL,
			PlottingProgress: plotRes.Progress,
		})
	}

	return plots, nil
}

func (c *Client) apiRequest(ctx context.Context, method string, endpoint string, body []byte) ([]byte, error) {

	var requestBody io.Reader
	if body != nil {
		requestBody = bytes.NewReader(body)
	}

	url := fmt.Sprintf("%s/%s", c.apiURL, endpoint)
	logrus.Debugf("making %s request to %s", method, url)

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

	res, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("invalid response received (%s)", res.Status)
	}

	responseBody, err := ioutil.ReadAll(res.Body)
	defer res.Body.Close()

	return responseBody, nil
}

func NewClient(apiKey, apiURL string) *Client {
	return &Client{
		apiKey: apiKey,
		apiURL: apiURL,
		client: http.DefaultClient,
	}
}
