.PHONY: build test lint demo demo-svg

build:
	go build -o ciblame ./cmd/ciblame

test:
	go test -race -count=1 ./...

lint:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)
	go vet ./...

demo:
	go run ./cmd/ciblame --demo

# Regenerates the README screenshot with a fixed window so it is reproducible.
demo-svg:
	go run ./cmd/ciblame --demo --until 2026-11-01 --color always | go run tools/ansi2svg/main.go > docs/demo.svg
