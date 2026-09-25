FROM golang:1.26-alpine AS build
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -mod=readonly -o /workspace/hermes ./cmd/hermes

FROM alpine:3.20
RUN addgroup -S hermes && adduser -S hermes -G hermes
WORKDIR /app
COPY --from=build --chown=hermes:hermes /workspace/hermes /app/hermes
USER hermes
EXPOSE 8080
ENTRYPOINT ["/app/hermes"]
