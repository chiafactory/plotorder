package client

//TODO: autogenerate from OpenAPI schema (https://chiafactory.com/static/openapi-schema.yml)

type updatePlotRequest struct {
	ID            string `json:"id"`
	State         string `json:"state"`
	DownloadState string `json:"download_state"`
}
