.PHONY: build test vet lint installer clean

MODULE := github.com/AlcanDev/korva-cli

build: ## Build the CLI binary into bin/korva
	go build -o bin/korva $(MODULE)/cmd/korva

test: ## Run tests with the race detector
	go test -race -count=1 -timeout 5m $(MODULE)/...

vet: ## Run go vet
	go vet $(MODULE)/...

lint: ## Run golangci-lint
	golangci-lint run --timeout=5m

installer: ## Cross-compile every binary into installer/dist/
	./installer/build.sh

clean: ## Remove build artifacts
	rm -rf bin installer/dist
