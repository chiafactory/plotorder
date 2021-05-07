from configparser import ConfigParser
from datetime import datetime
from os.path import dirname, join, realpath

config = ConfigParser()
config.read(join(realpath(dirname(__file__)), '../plotorder.conf'))

api_url = config['API']['api_url']
api_key = config['API']['api_key']
plot_dir = config['LOCAL']['plot_dir']
progress_file = config['LOCAL']['progress_file']


def get_time_elapsed_string(start_time: datetime) -> str:
    t = datetime.now()
    seconds_elapsed = int((t - start_time).total_seconds())
    minutes_elapsed = int(seconds_elapsed / 60) % 60
    hours_elapsed = int(seconds_elapsed / 3600)
    seconds_elapsed = seconds_elapsed % 60
    return (f'{hours_elapsed} h ' if hours_elapsed else '') + \
           (f'{minutes_elapsed} m ' if hours_elapsed or minutes_elapsed else '') + \
           f'{seconds_elapsed} s'
