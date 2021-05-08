"""The model for the plotorder downloader.

It contains Order (currently more-or-less useless), Plot with its download function and enumerators for states.
"""

from enum import Enum
from os.path import exists, join
import threading

import requests

from config.log import log


class Order:
    """The Order class."""
    def __init__(self, order_id: str) -> None:
        self.order_id = order_id

    def __repr__(self) -> str:
        return 'Order[id={}]'.format(self.order_id)


class PlotState(Enum):
    """The Plot State enumerator."""
    PENDING = 'P'
    PLOTTING = 'R'
    PUBLISHED = 'D'
    CANCELLED = 'C'
    EXPIRED = 'X'


class PlotDownloadState(Enum):
    """The Plot Download State enumerator."""
    NOT_STARTED = 0
    DOWNLOADING = 1
    DOWNLOADED = 2


class Plot:
    """The Plot class."""
    def __init__(self, plot_id: str, state: PlotState, plot_output_dir: str,
                 progress: int = 0, url: str = None,
                 download_state: PlotDownloadState = PlotDownloadState.NOT_STARTED) -> None:
        self.plot_id = plot_id
        self.url = url
        self.state = state
        self.progress = progress
        self.download_progress = None
        self.plot_size = None
        self.plot_output_dir = plot_output_dir
        self.download_state = download_state
        self.download_thread = threading.Thread(target=self._thread_download)
        self.kill_download = False

    def download(self) -> None:
        """Start the thread for downloading."""
        # self.download_thread = threading.Thread(target=self._thread_download)
        if self.url is None:
            log.warning(f'Can not download the plot ID={self.plot_id} since URL is not given!')
            return
        self.download_thread.start()

    def _thread_download(self) -> None:
        """The target method to be used for download thread."""
        # filename = self.url.split('/')[-1]  # Is an actual name important ?
        log.info(f'Starting thread_download for the plot ID={self.plot_id} ...')
        self.download_state = PlotDownloadState.DOWNLOADING
        try:
            with open(self.get_plot_filename(), 'ab') as f:
                data_downloaded = f.tell()
                if data_downloaded > 0:
                    log.info(f'Continuing download from {data_downloaded} byte on!')
                with requests.get(self.url, headers={'Range': f'bytes={data_downloaded}-'}, stream=True) as response:
                    if response.headers.get('Content-Type') == 'text/html':
                        log.info(f'Downloaded already over the plot size: '
                                 f'downloaded={data_downloaded}, plot_size={self.plot_size}')
                        self.download_state = PlotDownloadState.DOWNLOADED
                        log.debug(f'Set download_state={PlotDownloadState.DOWNLOADED}, returning.')
                        return  # TODO test if trying to resume already finished download.
                    self.plot_size = int(response.headers.get('Content-Length')) + data_downloaded
                    for data in response.iter_content(chunk_size=8192):
                        data_downloaded += len(data)
                        f.write(data)
                        self.download_progress = int(100 * data_downloaded / self.plot_size)
                        if self.kill_download:
                            log.info(f'Stopping the plot ID={self.plot_id} downloading!')
                            break
                    else:  # If no break neither interrupt, download_state will be set to 2 - complete.
                        self.download_state = PlotDownloadState.DOWNLOADED
                        log.info(f'Download of the plot ID={self.plot_id} complete!')
            log.debug(f'Re-setting kill_download flag for the plot ID={self.plot_id}.')
            self.kill_download = False
        except Exception as e:
            log.warning(f'Downloading of the plot ID={self.plot_id} failed!')
            log.exception(e)

    def stop_downloading(self) -> None:
        """Set kill_download flag so that the downloading thread will break."""
        self.kill_download = True

    def get_plot_filename(self) -> str:
        """Get the absolute path of the file which the plot should be stored in.

        If plot is not ready for download yet, return None.
        """
        if self.url is None:
            return None
        return join(self.plot_output_dir, self.url.split('/')[-1])

    def check_plot_file_exists(self) -> bool:
        """Check whether the plot's (partially) downloaded file exists."""
        filename = self.get_plot_filename()
        return filename is not None and exists(filename)

    def check_should_download(self) -> bool:
        """Return True if download has not started yet or if there exists (partially) downloaded file for it."""
        return self.download_state == PlotDownloadState.NOT_STARTED or self.check_plot_file_exists()

    def __repr__(self) -> str:
        return 'Plot[id={},state={}]'.format(self.plot_id, self.state)

    def __eq__(self, other) -> bool:
        return self.plot_id == other.plot_id
