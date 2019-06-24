.PHONY: build clean deploy prepare dynamoexport

build:
	export GO111MODULE=on
	env GOOS=linux go build -ldflags="-s -w" -o bin/eventsProcessor eventsProcessor/main.go

clean:
	rm -rf ./bin ./vendor Gopkg.lock

deploy: clean build
	sls deploy --verbose

prepare:
	go get -u ./...
	go mod vendor
	pip3 install export-dynamodb

dynamoexport:
	export-dynamodb -t skycatch-project-dev-image-data -f csv -o output.csv
