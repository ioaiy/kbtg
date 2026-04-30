# --- Build stage ---
FROM golang:1.22-alpine AS builder

WORKDIR /src

# Сначала копируем lock-файлы — это даёт кеш слоя `go mod download`,
# пока зависимости не меняются.
# go.sum обязателен; если его нет в репо — выполни `make tidy-docker`.
COPY go.mod go.sum ./
RUN go mod download

# Устанавливаем swag для генерации OpenAPI из аннотаций в коде.
# Версия совпадает с require в go.mod, чтобы CLI и runtime были совместимы.
RUN go install github.com/swaggo/swag/cmd/swag@v1.16.3

COPY . .

# Перегенерируем OpenAPI-доки из @Summary/@Param/@Success аннотаций
# в handlers — на каждый билд получаем свежие пути и схемы.
RUN swag init -g main.go -d ./cmd/server,./internal/httpapi,./internal/skinport -o ./docs --parseDependency --parseInternal

# CGO_ENABLED=0 + ldflags -s -w → статический бинарь без debug-инфы.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /out/server ./cmd/server

# --- Runtime stage ---
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/server /server

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/server"]
