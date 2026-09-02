.PHONY: build test vet run compose-up compose-down

BIN := bin/openusenet

build:
	mkdir -p bin
	go build -o $(BIN) ./cmd/openusenet

vet:
	go vet ./...

test:
	go test ./...

run: build
	./$(BIN) serve --config config.example.yml

compose-up:
	docker compose up --build -d

compose-down:
	docker compose down
