from datetime import datetime
import time

import click

from client import ApiClient
from config import api_key, api_url, get_time_elapsed_string, progress_file


@click.command()
@click.option('--order_id', prompt='Order ID', help='Order ID.')
@click.option('--force_download', default=0, help='Set to 1 if you would like to download also the plots that are '
                                                  'probably being downloaded by some other client.')
def start_processing_order(order_id, force_download):
    """Start processing the plots of a given order."""
    t0 = datetime.now()
    try:
        api_client = ApiClient(api_url, api_key, progress_file)
        # initialize plot objects list
        api_client.get_plots_for_order_id(order_id, True, force_download)
        # This method should be periodically executed to update
        # - progress status
        # - start new downloads, delete already downloaded plots, restart failed ...
        while True:
            api_client.proceed_with_plots()
            click.secho(f'Time elapsed: {get_time_elapsed_string(t0)}', bold=True)
            time.sleep(30)
    except KeyboardInterrupt:
        click.secho('')
        click.secho('   ABORTING ...', fg='red', bold=True)
        for plot in api_client.plots:
            while plot.download_thread.is_alive():
                plot.stop_downloading()
                time.sleep(1)


if __name__ == '__main__':
    start_processing_order()
