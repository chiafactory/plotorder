package client

//TODO: autogenerate from OpenAPI schema (https://chiafactory.com/static/openapi-schema.yml)

type orderResponse struct {
	ID string
}

type getOrdersResponse struct {
	Results []*orderResponse
}

type plotResponse struct {
	ID       string
	Progress int
	URL      string
	State    string
}

type getPlotsForOrderIDResponse struct {
	Plots []*plotResponse
}
