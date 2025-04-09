FROM golang:1.23 AS builder

WORKDIR /app

COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o main main.go

########################################################
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/main .
COPY templates/ /app/templates/

EXPOSE 8000

CMD ["./main"]