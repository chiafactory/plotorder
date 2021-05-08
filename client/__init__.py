"""The plotorder API client."""

import json
from typing import List

import click
import requests

from config.decorator import report_exception_issue
from config.log import log
from .model import Order, Plot, PlotDownloadState, PlotState


class ApiClient:
    def __init__(self, api_url: str, api_key: str, plot_output_dir: str, progress_file: str) -> None:
        self.api_url = api_url
        self.authorization_header = {'Authorization': f'Token {api_key}'}
        self.progress_file = progress_file
        self.plot_output_dir = plot_output_dir
        self.access_token = None
        self.refresh_token = None
        self.plots = []
        self.other_clients_count = 0

    def proceed_with_plots(self) -> None:
        """Periodically execute that method to proceed to the next stage with the plots being processed.

        The method also writes the progress report to the console and into self.progress_file.
        """
        for i in range(len(self.plots)):
            current_plot = self.plots[i]
            if current_plot.state == PlotState.PENDING or current_plot.state == PlotState.PLOTTING:
                updated_plot = self.get_plot(current_plot.plot_id)
                if updated_plot is not None:
                    self.plots[i] = updated_plot
            elif current_plot.state == PlotState.PUBLISHED:
                if current_plot.download_state == PlotDownloadState.NOT_STARTED:
                    # published, but not yet started - start downloading.
                    self.download_plot(current_plot)
                elif current_plot.download_state == PlotDownloadState.DOWNLOADING:
                    if not current_plot.download_thread.is_alive():
                        # download already started, but the downloading thread is dead; resume.
                        log.warning(f'Failed download of the plot ID={current_plot.plot_id}.')
                        updated_plot = self.get_plot(current_plot.plot_id)
                        if updated_plot is not None:
                            log.info(f'Re-initializing plot ID={updated_plot.plot_id} download.')
                            self.plots[i] = updated_plot
                            self.download_plot(updated_plot)
                elif current_plot.download_state == PlotDownloadState.DOWNLOADED:
                    # still published and already downloaded - delete.
                    log.info(f'Deleting the plot ID={current_plot.plot_id}.')
                    self.delete_plot(current_plot)
                else:
                    log.warning(f'Unsupported download state: {current_plot.download_state}.')
            elif current_plot.state == PlotState.CANCELLED or current_plot.state == PlotState.EXPIRED:
                pass
            else:
                log.warning(f'Unsupported plot state: {current_plot.state}. Re-setting.')
                updated_plot = self.get_plot(current_plot.plot_id)
                if updated_plot is not None:
                    self.plots[i] = updated_plot
        self._write_progress_report()

    @report_exception_issue
    def get_orders(self) -> List[Order]:
        """Get all the orders once ApiClient is authorized (i.e. tokens are set)."""
        orders = []
        response = requests.request('GET', self._compose_url('plot_orders'), headers=self.authorization_header).json()
        for order in response.get('results', []):
            orders.append(Order(order_id=order.get('id')))
        return orders

    @report_exception_issue
    def get_plots_for_order_id(self, order_id: str, rewrite: bool = False, force_download: bool = False) -> List[Plot]:
        """Get all the plots for the order with given order_id.

        :param rewrite: whether to store plots it got to self.plots or not.
        :param force_download: whether to download either if it should not.
        """
        plots = []
        response = requests.request('GET', self._compose_url('plot_orders', order_id),
                                    headers=self.authorization_header)
        self._check_response(response, order_id)
        other_clients_count = 0
        for plot in response.json().get('plots', []):
            p = Plot(plot_id=plot.get('id'),
                     state=PlotState(plot.get('state')),
                     plot_output_dir=self.plot_output_dir,
                     progress=plot.get('progress'),
                     url=plot.get('url'),
                     download_state=PlotDownloadState(plot.get('download_state', 0)))
            if not p.check_should_download() and not force_download:
                log.debug(f'Skipping the plot ID={p.plot_id} since someone else is handling it.')
                other_clients_count += 1
            else:
                plots.append(p)
        log.info(f'Found {len(plots)} plots for the order ID={order_id}, {other_clients_count} handled by other '
                 f'clients.')
        self.other_clients_count = other_clients_count
        if rewrite:
            self.plots = plots
        return plots

    def get_plots_for_order(self, order: Order, rewrite: bool = False,  force_download: bool = False) -> List[Plot]:
        """Get all the plots for the given order.

        :param rewrite: whether to store plots it got to self.plots or not.
        :param force_download: whether to download either if it should not.
        """
        return self.get_plots_for_order_id(order.order_id, rewrite, force_download)

    def check_plots_for_order_id(self, order_id: str) -> None:
        """Check whether some new plots appear or some plot disappear from the order with given ID.

        Add new plots and remove the disappeared ones if they are not just being downloaded.
        """
        current_plots = self.get_plots_for_order_id(order_id)
        if current_plots is not None:
            for plot in current_plots:
                if plot.plot_id not in [p.plot_id for p in self.plots]:
                    log.info(f'New plot ID={plot.plot_id} appeared on the order ID={order_id}')
                    self.plots.append(plot)
            plots_to_remove = []
            for plot in self.plots:
                if plot.plot_id not in [p.plot_id for p in current_plots]:
                    log.info(f'The plot ID={plot.plot_id} disappeared from the order ID={order_id}')
                    plots_to_remove.append(plot)
            for plot in plots_to_remove:
                if plot.download_thread.is_alive():
                    log.warning(f'The plot ID={plot.plot_id} disappeared from order ID={order_id} but it\'s still '
                                f'downloading; will leave it there.')
                else:
                    log.info(f'Removing the plot ID={plot.plot_id}.')
                    self.plots.remove(plot)

    def check_plots_for_order(self, order: Order) -> None:
        """Check whether some new plots appear or some plot disappear from the given order.

        Add new plots and remove the disappeared ones if they are not just being downloaded.
        """
        self.check_plots_for_order_id(order.order_id)

    @report_exception_issue
    def get_plot(self, plot_id: str) -> Plot:
        """Get plot with the given ID."""
        log.debug(f'Getting the plot ID={plot_id}.')
        response = requests.request('GET', self._compose_url('plots', plot_id),
                                    headers=self.authorization_header)
        self._check_response(response, plot_id)
        json_response = response.json()
        return Plot(plot_id=json_response.get('id'),
                    state=PlotState(json_response.get('state')),
                    plot_output_dir=self.plot_output_dir,
                    progress=json_response.get('progress'),
                    url=json_response.get('url'),
                    download_state=PlotDownloadState(json_response.get('download_state', 0)))

    @report_exception_issue
    def download_plot(self, plot) -> None:
        """Check whether plot is published and download if that's the case."""
        if plot.state != PlotState.PUBLISHED:
            log.error(f'Inappropriate state: {plot.state}. Can only download {PlotState.PUBLISHED}.')
            return
        report_downloading = False
        if plot.download_state != PlotDownloadState.DOWNLOADING:
            # In this case, downloading is being started for the first time.
            log.debug(f'The plot ID={plot.plot_id} is not yet reported downloading!')
            report_downloading = True
        if not plot.download_thread.is_alive():
            plot.download()
        else:
            log.warning(f'The plot with ID={plot.plot_id} is already downloading!')
        if report_downloading:
            self.report_downloading(plot)

    @report_exception_issue
    def report_downloading(self, plot):
        """Send HTTP PUT request to change plot's download_state."""
        log.info(f'Reporting the plot ID={plot.plot_id} download state via API.')
        payload = {'id': plot.plot_id, 'download_state': PlotDownloadState.DOWNLOADING.value}
        headers = {'Accept': 'application/json', 'Content-Type': 'application/json'}
        headers.update(self.authorization_header)
        response = requests.request(
            'PUT', self._compose_url('plots', plot.plot_id), headers=headers, data=json.dumps(payload)
        )  # this response contains plot object just being updated.
        self._check_response(response, plot.plot_id)
        json_response = response.json()
        if json_response.get('id') != plot.plot_id:
            log.warning(f'JSON response to PUT plots/{plot.plot_id} does not contain appropriate ID!')
            log.debug(json_response)

    @report_exception_issue
    def delete_plot(self, plot) -> None:
        if plot.download_state == PlotDownloadState.DOWNLOADED:
            payload = {'id': plot.plot_id, 'state': PlotState.EXPIRED.value,
                       'download_state': plot.download_state.value}
            headers = {'Accept': 'application/json', 'Content-Type': 'application/json'}
            headers.update(self.authorization_header)
            response = requests.request(
                'PUT', self._compose_url('plots', plot.plot_id), headers=headers, data=json.dumps(payload)
            )  # this response contains plot object just being updated.
            self._check_response(response, plot.plot_id)
            json_response = response.json()
            if json_response.get('id') != plot.plot_id:
                log.warning(f'JSON response to PUT plots/{plot.plot_id} does not contain appropriate ID!')
                log.debug(json_response)
            else:
                plot.state = PlotState.EXPIRED
        else:
            log.warning(f'Should not delete not-yet-downloaded plot (ID={plot.plot_id})!')

    def _write_progress_report(self, dump_to_file: bool = True) -> None:
        """Write progress report to console and to file if dump_to_file evaluates to True."""
        click.clear()
        msg1 = f'All plots: {len(self.plots) + self.other_clients_count}\n' \
               f'Handled by other clients: {self.other_clients_count}'
        click.secho(msg1, fg='cyan')

        msg2 = f'Pending plots: {len([x for x in self.plots if x.state == PlotState.PENDING])}'
        click.secho(msg2, fg='yellow')

        active = [x for x in self.plots if x.state == PlotState.PLOTTING]
        msg3 = f'Active: {len(active)}:'
        click.secho(msg3, fg='green')

        progress_messages = [msg1, msg2, msg3]
        for p in active:
            msg = f'    * {p.plot_id}: plotting {p.progress:3d}%'
            click.secho(msg, fg='green')
            progress_messages.append(msg)

        downloading = [x for x in self.plots if x.state == PlotState.PUBLISHED]
        msg4 = f'Downloading: {len(downloading)}'
        click.secho(msg4, fg='red')
        progress_messages.append(msg4)
        for p in downloading:
            if p.download_state == PlotDownloadState.NOT_STARTED or p.download_progress is None:
                msg = f'    * {p.plot_id}: download is going to start!'
            else:
                msg = f'    * {p.plot_id}: downloaded {p.download_progress:3d}% {p.download_speed:>16}'
            click.secho(msg, fg='red')
            progress_messages.append(msg)
        msg5 = f'Expired plots: {len([x for x in self.plots if x.state == PlotState.EXPIRED])}\n' \
               f'Canceled plots: {len([x for x in self.plots if x.state == PlotState.CANCELLED])}'
        click.secho(msg5, fg='white')
        progress_messages.append(msg5)
        if dump_to_file:
            with open(self.progress_file, 'w') as f:
                f.write('\n'.join(progress_messages) + '\n')

    def _compose_url(self, *args) -> str:
        """Compose url with the given path parameters."""
        return '/'.join(map(lambda x: str(x).rstrip('/'), [self.api_url, *args])) + '/'

    @staticmethod
    def _check_response(response, request_id: str):
        """Check the response from HTTP request and raise exception if response code is not 200."""
        if response.status_code != 200:
            log.warning(f'Non-200 status code while getting plots for order ID={request_id}: {response.status_code}.')
            response_text = response.text
            log.debug(response_text)
            raise ConnectionError(f'Error while getting plots for order ID={request_id}: '
                                  f'http_status={response.status_code}, response={response_text}')

    def _set_tokens(self, username: str, password: str) -> None:
        """Set access and refresh tokens."""
        log.warning(f'Setting tokens method is deprecated!')
        payload = {"username": username, "password": password}
        headers = {
            'Accept': 'application/json',
            'Content-Type': 'application/json'
        }
        response = requests.request(
            'POST', self._compose_url('token'), headers=headers, data=json.dumps(payload)
        ).json()
        self.access_token = response.get('access')
        self.refresh_token = response.get('refresh')
