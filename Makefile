LDFLAGS = -s -w -X github.com/Bnei-Baruch/archive-stt/version.PreRelease=$(PRE_RELEASE)

build: test
	go build -trimpath -ldflags '$(LDFLAGS)' -o archive-stt .

# the server's binary (ai-tools: linux/amd64)
build-linux: test
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o archive-stt-linux-amd64 .

test:
	go vet ./...
	go test -race -count=1 ./...

fmt:
	gofmt -w .

clean:
	rm -f archive-stt archive-stt-linux-amd64

.PHONY: build build-linux test fmt clean
