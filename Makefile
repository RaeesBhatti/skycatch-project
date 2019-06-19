.PHONY: build clean deploy gomod

build:
	export GO111MODULE=on
	env GOOS=linux go build -ldflags="-s -w" -o bin/service service/main.go

clean:
	rm -rf ./bin ./vendor Gopkg.lock

deploy: clean build
	sls deploy --verbose

gomod:
	go get -u ./...
	go mod vendor
