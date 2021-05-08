"""The plotorder downloader configuration stuff."""

from configparser import ConfigParser
from datetime import datetime
from os.path import dirname, isabs, join, realpath
import sys

import click


config = ConfigParser()
config.read(join(realpath(dirname(__file__)), '../plotorder.conf'))

try:
    api_url = config['API']['api_url']
    api_key = config['API']['api_key']
except KeyError:
    click.secho('Missing api_url or api_key in config API section. Fix it and rerun!', fg='red', bold=True)
    sys.exit(1)
default_progress_file = join(realpath(dirname(__file__)), '../progress.file')
try:
    local_config = config['LOCAL']
    plot_output_dir = local_config.get('plot_dir')
    progress_file = local_config.get('progress_file', default_progress_file)
except KeyError:
    plot_output_dir, progress_file = None, default_progress_file


class Utils:
    """General helper methods."""
    @staticmethod
    def get_plot_output_dir(plot_dir):
        """Given the plot_dir parameter, construct an absolute path for that file.

        If plot_dir is None, use the plot_output_dir, extracted from plot_dir property in configuration. If even that one
        is None, report an issue.
        """
        if plot_dir is None:
            if plot_output_dir is None:
                click.secho('Set --plot_dir parameter or define the absolute path in plotorder.conf!', fg='red', bold=True)
                sys.exit(1)
            else:
                return plot_output_dir
        return plot_dir if isabs(plot_dir) else join(realpath(dirname(__file__)), '..', plot_dir)

    @staticmethod
    def get_time_elapsed_string(start_time: datetime) -> str:
        """Return duration from start time to this moment in format 2 h 31 m 24 s."""
        t = datetime.now()
        seconds_elapsed = int((t - start_time).total_seconds())
        minutes_elapsed = int(seconds_elapsed / 60) % 60
        hours_elapsed = int(seconds_elapsed / 3600)
        seconds_elapsed = seconds_elapsed % 60
        return (f'{hours_elapsed} h ' if hours_elapsed else '') + \
               (f'{minutes_elapsed} m ' if hours_elapsed or minutes_elapsed else '') + \
               f'{seconds_elapsed} s'
