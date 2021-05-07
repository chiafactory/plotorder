# plotorder
Create and download Chia plots on demand using [Chia Factory](https://chiafactory.com) service.

It's using [Chia Factory API](https://chiafactory.com/api/) and streamlines whole download process.

## Requirements
- python 3.7+
- Windows/Linux/Mac OSx

## Usage

```sh
$ ~ plotorder login
> Username: chiafan
> Password: **********

$ ~ plotorder create
Opening browser to Chia Factory...
Order #5GasF received

$ ~ plotorder download --check-after-download --expire-after-check 5GasF
Downloading order #5GasF
Farmer pk: ....
Pool pk: ....
Status: Running
All Plots: 40 
 Pending: 30 plots 
 Active:
   #aFasF - Plotting - [ -----45%      ]
   #zFzsG - Plotting - [ -12%          ]
 Expired: 5 plots
Downloaded (5%):
 #zFzsG
 #5GAsH

## Script details

Pass order_id into the script.

Fill the credentials in config/__init__.py-template and copy to config/__init__.py

Run the script and periodically execute api_client.proceed_with_plots() to update progress file and to proceed to the next stage of the plot,
e.g. from PUBLISHED to DOWNLOADING, to delete downloaded, etc.
