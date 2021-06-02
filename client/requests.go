package client

type updatePlotRequest struct {
	ID            string `json:"id"`
	State         string `json:"state"`
	DownloadState string `json:"download_state"`
}
