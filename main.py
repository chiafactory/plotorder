"""The main script to be executed to process an order."""

from datetime import datetime
import time

import click

from client import ApiClient
from config import Utils, api_key, api_url, progress_file


@click.command()
@click.option('--order_id', prompt='Order ID', help='The order ID to be processed.')
@click.option('--plot_dir', default=None, help='The path where to store downloaded plots. '
                                               'It overrides plot_dir from conf file')
@click.option('--refresh_period', default=30, help='The plots state refresh period in seconds. Default 30.')
@click.option('--force_download', default=0, help='Set to 1 if you would like to download also the plots that are '
                                                  'probably being downloaded by some other client. Default 0.')
def start_processing_order(order_id, plot_dir, refresh_period, force_download):
    """Start processing the plots of a given order."""
    t0 = datetime.now()
    try:
        api_client = ApiClient(api_url, api_key, Utils.get_plot_output_dir(plot_dir), progress_file)
        # initialize plot objects list
        api_client.get_plots_for_order_id(order_id, True, force_download)
        # This method should be periodically executed to update
        # - progress status
        # - start new downloads, delete already downloaded plots, restart failed ...
        while True:
            api_client.proceed_with_plots()
            click.secho(f'Time elapsed: {Utils.get_time_elapsed_string(t0)}', bold=True)
            time.sleep(refresh_period)
    except KeyboardInterrupt:
        click.secho('')
        click.secho('   ABORTING ...', fg='red', bold=True)
        for plot in api_client.plots:
            while plot.download_thread.is_alive():
                plot.stop_downloading()
                time.sleep(1)


if __name__ == '__main__':
    start_processing_order()
