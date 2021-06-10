package client

//TODO: autogenerate from OpenAPI schema (https://chiafactory.com/static/openapi-schema.yml)

type orderResponse struct {
	ID string `json:"id"`
}

type getOrdersResponse struct {
	Results []*orderResponse
}

type plotResponse struct {
	ID       string `json:"id"`
	Progress int    `json:"progress"`
	URL      string `json:"url"`
	State    string `json:"state"`
}

type getPlotsForOrderIDResponse struct {
	Plots []*plotResponse
}
