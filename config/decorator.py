"""The decorators used in plotorder downloader."""

import time
import sys
from collections import Callable

import click

from . import exception_retry_count, exception_retry_wait
from .log import log


def report_exception_issue(method: Callable,
                           retry_wait: int = exception_retry_wait, retry_count: int = exception_retry_count):
    """Report an issute and retry executing the method.

    If an exception occurred while executing the method, retry execution for retry_count times and wait retry_wait
    seconds between each two executions.
    """
    def wrapper(*args, **kwargs):
        c = 1
        while True:
            try:
                return method(*args, **kwargs)
            except Exception as e:
                c += 1
                _handle_exception(e, method.__name__, -1 if c > retry_count else retry_wait)
    return wrapper


def _handle_exception(exception: Exception, method_name: str, retry_wait: int):
    """Report an exception and exit the execution if retry_wait is non-positive."""
    if retry_wait > 0:
        log.exception(f'Error while executing {method_name}')
        click.secho(f'\nException occurred:\n{str(exception)}\nWill wait for {retry_wait} s and then retry.',
                    fg='red', bold=True)
        time.sleep(retry_wait)
    else:
        log.warning('Retried too many times, will exit.')
        click.secho(f'\nRetried too many times, exiting. Bye bye!', fg='red', bold=True)
        sys.exit()
