from client import ApiClient
from config import *

order_id = '***'

api_client = ApiClient(api_url, api_key, progress_file)
# initialize plot objects list
api_client.get_plots_for_order_id(order_id, True)
# Should set force_download=True if the other client downloading them crashed
# and is not going to resume.

# TODO : gracefully stopping downloads if something goes wrong.

# This method should be periodically executed to update
# - progress status
# - start new downloads, delete already downloaded plots, restart failed ...
api_client.proceed_with_plots()
