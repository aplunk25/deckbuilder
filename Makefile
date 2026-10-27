BINARY  := deckbuilder
IMAGE   := deckbuilder
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: all build lambda api-local test cover fmt fmt-check vet lint tf-check ci docker-build docker-run clean

all: ci build

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) .

# Lambda binaries: linux/arm64, named "bootstrap" for the provided.al2023 runtime.
lambda:
	for fn in api sync; do \
		GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -tags lambda.norpc \
			-ldflags "-s -w" -o build/$$fn-lambda/bootstrap ./cmd/$$fn-lambda || exit 1; \
	done

# Run the API locally against the CLI's card cache.
api-local:
	CARDS_FILE=$${RIFTBOUND_HOME:-$$HOME/.riftbound}/cards.json go run ./cmd/api-lambda

test:
	go test -race ./...

cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

fmt:
	gofmt -w .

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "needs gofmt:"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

lint:
	golangci-lint run

tf-check:
	terraform -chdir=infra fmt -check -recursive
	terraform -chdir=infra init -backend=false -input=false >/dev/null
	terraform -chdir=infra validate

# Everything CI runs, for checking locally before pushing.
ci: fmt-check vet lint test

docker-build:
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

# Example: make docker-run ARGS="search -type legend"
docker-run:
	docker run --rm -it -v riftbound-data:/data $(IMAGE):latest $(ARGS)

clean:
	rm -rf $(BINARY) build dist coverage.out
