# plotorder

Automate download of Chia plots from
[Chia Factory](https://chiafactory.com) service.

It's using [Chia Factory API](https://chiafactory.com/api/) and streamlines
whole download process.

## Requirements

Binaries are published for `Windows`, `Darwin` (macOS, OSX...) and `Linux` based operating systems with 64bit architectures. You can also build `plotorder` for other OS/arch very easily. Check out the `Makefile` for examples (see [this](https://golang.org/doc/install/source#environment) for available OS/arch compile targets)

## Usage

The recommended way to use `plotorder` is to download the latest release binaries. For simplicity, you can use any of the following snippets to do it, which will create a `plotorder` executable binary in your current working directory.

**Linux**
```shell
curl -s https://api.github.com/repos/chiafactory/plotorder/releases/latest | grep "browser_download_url.*plotorder-linux-amd64" | cut -d '"' -f 4 | xargs curl -Ls --output plotorder
```
**macOS**
```shell
curl -s https://api.github.com/repos/chiafactory/plotorder/releases/latest | grep "browser_download_url.*plotorder-macos-amd64" | cut -d '"' -f 4 | xargs curl -Ls --output plotorder
```
**Windows**
```powershell
Invoke-WebRequest -Uri $((((Invoke-WebRequest -Uri ‘https://api.github.com/repos/chiafactory/plotorder/releases/latest').Content | ConvertFrom-Json).assets.browser_download_url | select-string -Pattern 'pandoc-2.14.0.1-1-amd64.deb’).Line) -OutFile plotorder.exe
```

You can also find all the binaries [here](https://github.com/chiafactory/plotorder/releases/)

Alternatively, if you have `GO` installed and prefer building and running it by yourself, clone this repo and do `go run ./`.

Once you download `plotorder`, you can get started very quickly:

**Linux** and **macOS**
```shell
./plotorder --api-key YOUR_API_KEY --order-id ORDER_ID
```

**Windows**
```powershell
Start-Process -NoNewWindow -FilePath "plotorder.exe" -ArgumentList "-api-key:YOUR_API_KEY","-order-id:ORDER_ID"
```

Below is a list of all the avaialable arguments.
| Name                   | Required | Description                                       | Default                        |
|------------------------|----------|---------------------------------------------------|--------------------------------|
| --api-key              | yes      | your personal https://chiafactory.com API key     | N/A                            |
| --order-id             | yes      | the id of the order you want to process plots for | N/A                            |
| --api-url              | no       | the URL of Chiafactory's API                      | https://chiafactory.com/api/v1 |
| --plot-dir             | no       | the path where to store downloaded plots          | `plots/` in current directory  |
| --plot-check-frequency | no       | the time between checks on an order's plots       | `2s`                           |
| --config               | no       | config file to use                                | N/A                            |
| --verbose              | no       | enables verbose logging (DEBUG level)             | `false`                        |

A sample config file is included in this repo (`config.example.ini`). You can use the config file instead of providing the CLI arguments above. CLI arguments will override matching config file values (except for `order-id`, `config` and `verbose` )

You can interrupt the download process at any point in time and `plotorder` will gracefully exit. You can later on resume downloading by running `plotorder` with the same arguments and/or config file (specially, the same `order-id` and `plot-dir`).
