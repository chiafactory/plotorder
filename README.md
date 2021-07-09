# plotorder

This is the recommended way of downloading plots from [Chia Factory](https://chiafactory.com) plotting service.

It's using [Chia Factory API](https://chiafactory.com/api/) and streamlines the whole process.

## Features
 - **Blake2 checksums** on every 10GB plot chunk to avoid bad downloads
 - **Multiple plot directories** and will populate them automatically depending on spare space
 - **Parallel downloads** to maximise your bandwidth
 - **Download Resume** 
 - **Binaries for most OSes** to get you going ASAP.


## Usage

The recommended way to use `plotorder` is to download the latest release binaries. 

**Linux** and **macOS**
```shell
./plotorder --api-key YOUR_API_KEY --order-id ORDER_ID
```

**Windows**
```powershell
Start-Process -NoNewWindow -FilePath "plotorder.exe" -ArgumentList "-api-key:YOUR_API_KEY","-order-id:ORDER_ID"
```

# Install 

You can find all the binaries [here](https://github.com/chiafactory/plotorder/releases/)

Binaries are published for `Windows`, `Darwin` (macOS, OSX...) and `Linux` based operating systems with 64bit architectures. 

*For simplicity*, you can use any of the following snippets to do it, which will create a `plotorder` executable binary in your current working directory.

**Linux (amd64)**
```shell
curl -s https://api.github.com/repos/chiafactory/plotorder/releases/latest | grep "browser_download_url.*plotorder-linux-amd64" | cut -d '"' -f 4 | xargs curl -Ls --output plotorder && chmod +x plotorder
```
**Linux (arm64)**
```shell
curl -s https://api.github.com/repos/chiafactory/plotorder/releases/latest | grep "browser_download_url.*plotorder-linux-arm64" | cut -d '"' -f 4 | xargs curl -Ls --output plotorder && chmod +x plotorder
```

**macOS (Intel)**
```shell
curl -s https://api.github.com/repos/chiafactory/plotorder/releases/latest | grep "browser_download_url.*plotorder-darwin-amd64" | cut -d '"' -f 4 | xargs curl -Ls --output plotorder && chmod +x plotorder
```
**macOS (Apple Silicon)**
```shell
curl -s https://api.github.com/repos/chiafactory/plotorder/releases/latest | grep "browser_download_url.*plotorder-darwin-arm64" | cut -d '"' -f 4 | xargs curl -Ls --output plotorder && chmod +x plotorder
```

**Windows** (powershell)
```powershell
Invoke-WebRequest -Uri $((((Invoke-WebRequest -Uri ‘https://api.github.com/repos/chiafactory/plotorder/releases/latest').Content | ConvertFrom-Json).assets.browser_download_url | select-string -Pattern 'pandoc-2.14.0.1-1-amd64.deb’).Line) -OutFile plotorder.exe
```


## Configuration

Below is a list of all the avaialable arguments.
| Name                   | Required | Description                                       | Default                        |
|------------------------|----------|---------------------------------------------------|--------------------------------|
| --api-key              | yes      | your personal https://chiafactory.com API key     | N/A                            |
| --order-id             | yes      | the id of the order you want to process plots for | N/A                            |
| --api-url              | no       | the URL of Chiafactory's API                      | https://chiafactory.com/api/v1 |
| --logs-dir             | no       | the directory to store logs                       | `logs/` in working directory   |
| --plot-dir             | no       | the directory to download plots (multiple allowed)| `plots/` in working directory  |
| --plot-check-frequency | no       | the time between checks on an order's plots       | `5s`                           |
| --max-downloads        | no       | maximum number of downloads in parallel           | `0` (unlimited)                |
| --config               | no       | config file to use                                | N/A                            |
| --verbose              | no       | enables verbose logging (DEBUG level)             | `false`                        |

A sample config file is included in this repo (`config.example.ini`). You can use the config file instead of providing the CLI arguments above. CLI arguments will override matching config file values (except for `order-id`, `config` and `verbose` )

You can interrupt the download process at any point in time and `plotorder` will shutdown gracefully. You can then resume downloading by running `plotorder` with the same arguments and/or config file (specially, the same `order-id` and `plot-dir`).

### Support for multiple download locations
You can provide `--plot-dir` multiple times (for instance, if you have multiple drives mounted). `plotorder` will try to fill all the provided directories with the downloaded plot files. If there's not enough space in a given directory, `plotorder` will skip to the next, until it finds a valid one.

There's an exception to this rule though: if a plot file has been partially downloaded to a specific directory, `plotorder` will always choose the same directory, so the file download resumes.

If there's not enough space to download all the plots (the published ones), `plotorder` will exit.

### Logging
By default, logs are stored under `plots/` in the working directory. You can specify a different path using `--logs-dir`. Logs will be auto-rotated when they reach 256MB. Old log files are compressed and retained for 30 days.

## Build

You can also build `plotorder` for other OS/arch very easily. Check out the `Makefile` for examples (see [this](https://golang.org/doc/install/source#environment) for available OS/arch compile targets)
