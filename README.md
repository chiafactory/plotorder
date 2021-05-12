# plotorder

Automate download of Chia plots from
[Chia Factory](https://chiafactory.com) service.

It's using [Chia Factory API](https://chiafactory.com/api/) and streamlines
whole download process.


## Requirements

- python 3.7+
- Windows/Linux/Mac OSx


## Script details

Create python 3.7+ virtual environment and install pip requirements:
```
pip install -r requirements.txt
```

Create `log` folder as a sibling of `main.py` script.

Copy `plotorder.conf.template` into `plotorder.conf` and appropriately set 
the credentials for the API and local paths.

Run the script `main.py`:

```
python main.py --help
```

Basically, the parameter you need to pass is order_id and the script will
handle the plots from the order with the given ID.

```
python main.py --order_id=XYZ
```

Keyboard interruption will gracefully stop all the downloads.

You can run the script on multiple machines or on the same machine multiple
instances of a script (which doesn't really make much sense since one script
can process all the plots from one order and the speed is mostly limited by
the network bandwidth) but in that case, the instances of a script should use
**distint output folders!**

Some manual intervention may be needed for multi-node execution in order for
nodes to divide work. Once one node starts downloading a specific plot, that
plot won't be processed by any other node.

### Final notes

* When the script starts, it may show 0% downloaded at the first check even if
the plot is partially downloaded already since download starts in a new thread
which may not be synchronized with the reporting message at the beginning.
* Once the download is complete, it may happen that a warning appear in log
files / on console output that a download failed since a thread is not yet
synchronized with the application state. In the next check, the download
should be finished and the plot set as expired. 
