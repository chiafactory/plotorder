.PHONY: build-linux build-macos build-windows build

build-linux:
	@mkdir -p build && GOOS=linux GOARCH=amd64 go build -o build/plotorder-linux-amd64

build-macos:
	@mkdir -p build && GOOS=darwin GOARCH=amd64 go build -o build/plotorder-macos-amd64

build-windows:
	@mkdir -p build && GOOS=windows GOARCH=amd64 go build -o build/plotorder-windows-amd64.exe

build: build-macos build-linux build-windows
