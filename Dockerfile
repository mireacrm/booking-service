FROM golang:1.26-alpine AS builder

WORKDIR /src

# Контракты и общий обвяз — отдельные модули, приезжают из сети по версии.
# Слой с зависимостями отделён от кода: он меняется только при повышении
# версии в go.mod и переиспользуется между сборками.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/booking ./cmd/booking

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/booking /booking
EXPOSE 8003 9003
USER nonroot:nonroot
ENTRYPOINT ["/booking"]
