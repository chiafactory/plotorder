#!/bin/bash
set -u

abort() {
  printf "%s\n" "$@"
  exit 1
}

if [ -z "${BASH_VERSION:-}" ]; then
  abort "You need bash in order to run this script."
fi

# These two will be populated by the server
API_KEY=__API_KEY__
ORDER_ID=__ORDER_ID__

if [[ "$API_KEY" == __API_KEY__ ]]; then
  abort "__API_KEY__ not given"
fi

if [[ "$ORDER_ID" == __ORDER_ID__ ]]; then
  abort "__ORDER_ID__ not given"
fi

OS=""
ARCH=""
OSNAME="$(uname)"
MACHINE="$(uname -m)"
if [[ "$OSNAME" == "Linux" ]]; then
  OS="linux"
  if [[ "$MACHINE" == "x86_64" ]]; then
    ARCH="amd64"
  elif [[ "$MACHINE" == "arm64" ]]; then
    ARCH="arm64"
  else
    abort "Unknown arch ${MACHINE}. Please install it manually (https://github.com/chiafactory/plotorder/releases) or report a bug."
  fi
elif [[ "$OSNAME" == "Darwin" ]]; then
  OS="darwin"
  if [[ "$MACHINE" == "arm64" ]]; then
    ARCH="arm64"
  else
    ARCH="amd64"
  fi
else
  abort "This script can only run in Darwin and Linux based operating systems."
fi

CURL="$(which curl)"
if [ -z "${CURL:-}" ]; then
  abort "cURL is required to run this script"
fi

FILENAME="plotorder-${OS}-${ARCH}"

echo "Downloading plotorder (${FILENAME})"
curl -s https://api.github.com/repos/chiafactory/plotorder/releases/latest \
  | grep "browser_download_url.*${FILENAME}" \
  | cut -d '"' -f 4 \
  | xargs curl -Ls --output /usr/local/bin/plotorder

if [[ "$?" != 0 ]]; then
  abort "There was an error downloading the binary."
fi

if [ ! -f /usr/local/bin/plotorder ]; then
  abort "The plotorder binary cannot be found. Please try downloading it manually (https://github.com/chiafactory/plotorder/releases)."
fi

chmod +x /usr/local/bin/plotorder
/usr/local/bin/plotorder --api-key=API_KEY --order-id=ORDER_ID
