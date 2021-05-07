from client import ApiClient
from config import *

order_id = '***'

api_client = ApiClient(api_url, api_key, progress_file)
# initialize plot objects list
api_client.get_plots_for_order_id(order_id, True)

# TODO : try / catch. If exception, stop_downloading all the plots.

api_client.proceed_with_plots()
