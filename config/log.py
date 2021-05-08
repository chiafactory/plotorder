"""The logging configuration."""

import logging as log
from os.path import dirname, join, realpath

dir_path = dirname(realpath(__file__))

log.basicConfig(format='%(asctime)s [%(module)s:%(lineno)s - %(levelname)s]: %(message)s',
                datefmt='%Y-%m-%d %H:%M:%S %Z',
                filename=join(dir_path, '../log/chia.log'),
                level=log.DEBUG)
log.getLogger('requests').setLevel(log.WARNING)
log.getLogger('urllib3').setLevel(log.WARNING)
