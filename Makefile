.PHONY: help run build test test-integration clean-test-cache tidy tidy-docker swag swag-docker migrate-up migrate-down docker-build docker-up docker-up-fg docker-stop docker-down docker-logs lint

# Загружаем .env, если он есть, чтобы make-таргеты видели переменные.
ifneq (,$(wildcard ./.env))
include .env
export
endif

DATABASE_URL ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@$(POSTGRES_HOST):$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=$(POSTGRES_SSLMODE)

# Если Go установлен локально — гоняем команды напрямую; иначе через Docker.
# Затрагивает test/test-integration — самые часто запускаемые таргеты.
HAS_GO := $(shell command -v go 2>/dev/null)
GO_IMAGE := golang:1.22

# Persistent named volumes для Go-кеша зависимостей и build-артефактов.
# Первый запуск качает ~80MB, последующие — мгновенные. Очистить:
# docker volume rm kbtg-go-mod kbtg-go-build
GO_CACHE_VOLUMES := -v kbtg-go-mod:/go/pkg/mod -v kbtg-go-build:/root/.cache/go-build

help: ## Показать список команд
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-22s %s\n", $$1, $$2}'

run: ## Запустить сервис локально (требует поднятых postgres + redis)
	go run ./cmd/server

build: ## Собрать бинарь в ./bin/server
	CGO_ENABLED=0 go build -ldflags="-s -w" -o ./bin/server ./cmd/server

test: ## Unit-тесты (локально если Go установлен, иначе в Docker с кешем)
ifneq ($(HAS_GO),)
	go test -race -count=1 ./...
else
	docker run --rm \
		-v $(PWD):/src -w /src \
		$(GO_CACHE_VOLUMES) \
		$(GO_IMAGE) \
		go test -race -count=1 ./...
endif

test-integration: ## Integration-тесты (testcontainers; локально или в Docker через docker.sock)
ifneq ($(HAS_GO),)
	go test -race -count=1 -tags=integration ./...
else
	docker run --rm \
		-v $(PWD):/src -w /src \
		-v /var/run/docker.sock:/var/run/docker.sock \
		$(GO_CACHE_VOLUMES) \
		-e TESTCONTAINERS_HOST_OVERRIDE=host.docker.internal \
		$(GO_IMAGE) \
		go test -race -count=1 -tags=integration ./...
endif

clean-test-cache: ## Очистить Docker-кеш Go-зависимостей (форс пересборку)
	docker volume rm kbtg-go-mod kbtg-go-build 2>/dev/null || true

tidy: ## go mod tidy локально (требует установленный Go)
	go mod tidy

tidy-docker: ## go mod tidy внутри Docker (если Go локально не установлен)
	docker run --rm -v $(PWD):/src -w /src golang:1.22-alpine go mod tidy

swag: ## Сгенерировать OpenAPI-доки локально (требует swag CLI)
	swag init -g cmd/server/main.go -o ./docs --parseDependency --parseInternal

swag-docker: ## Сгенерировать OpenAPI внутри Docker (если swag CLI не установлен)
	docker run --rm -v $(PWD):/src -w /src golang:1.22-alpine sh -c \
		"go install github.com/swaggo/swag/cmd/swag@v1.16.3 && swag init -g cmd/server/main.go -o ./docs --parseDependency --parseInternal"

migrate-up: ## Применить миграции
	migrate -path ./migrations -database "$(DATABASE_URL)" up

migrate-down: ## Откатить последнюю миграцию
	migrate -path ./migrations -database "$(DATABASE_URL)" down 1

docker-build: ## Собрать образы docker compose
	docker compose -f deployments/docker-compose.yml --env-file .env build

docker-up: ## Поднять стек в detached-режиме (фоном). Перед первым запуском — make docker-build
	docker compose -f deployments/docker-compose.yml --env-file .env up -d

docker-up-fg: ## Поднять стек в foreground (логи в терминал, Ctrl+C для остановки)
	docker compose -f deployments/docker-compose.yml --env-file .env up

docker-logs: ## Хвост логов всех сервисов (`make docker-logs` или с именем: `make docker-logs s=app`)
	docker compose -f deployments/docker-compose.yml logs -f $(s)

docker-stop: ## Поставить на паузу (контейнеры остаются, `make docker-up` быстро поднимет)
	docker compose -f deployments/docker-compose.yml stop

docker-down: ## Полный teardown: контейнеры + volumes (БД обнулится, миграции прогонятся заново)
	docker compose -f deployments/docker-compose.yml down -v

lint: ## go vet (минимальный лайнт без внешних tool'ов)
	go vet ./...
