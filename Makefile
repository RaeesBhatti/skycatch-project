.PHONY: build clean deploy prepare

build:
	export GO111MODULE=on
	env GOOS=linux go build -ldflags="-s -w" -o bin/imageProcessor imageProcessor/main.go
	env GOOS=linux go build -ldflags="-s -w" -o bin/csvExporter csvExporter/main.go

clean:
	rm -rf ./bin outfile

deploy: prepare clean build
	sls deploy --verbose

prepare:
	go get -u ./...
	go mod vendor
