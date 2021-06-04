.PHONY: build-linux-amd build-linux-arm build-darwin-amd build-darwin-arm build-windows build

buildir:
	@mkdir -p build 

build-linux-amd: buildir
	@GOOS=linux GOARCH=amd64 go build -o build/plotorder-linux-amd64

build-linux-arm: buildir
	@GOOS=linux GOARCH=arm64 go build -o build/plotorder-linux-arm64

build-darwin-amd: buildir
	@GOOS=darwin GOARCH=amd64 go build -o build/plotorder-darwin-amd64

build-darwin-arm: buildir
	@GOOS=darwin GOARCH=arm64 go build -o build/plotorder-darwin-arm64

build-windows: buildir
	@GOOS=windows GOARCH=amd64 go build -o build/plotorder-windows-amd64.exe

build: build-linux-amd build-linux-arm build-darwin-amd build-darwin-arm build-windows
