# Runtime Dockerfile for task139-railsignal (Go + embedded native frontend).
# Build with: docker buildx build --platform linux/amd64 --load -t go-task-check:amd64 .
FROM docker.m.daocloud.io/library/golang:1.26.3-bookworm AS build
WORKDIR /src
ENV GOTOOLCHAIN=local \
    GOPROXY=https://goproxy.cn,direct \
    GOSUMDB=sum.golang.google.cn \
    CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/railsignal .

FROM docker.m.daocloud.io/library/alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/railsignal /app/railsignal
ENV DB_PATH=/data/railsignal.db
EXPOSE 8080
# Default smoke-test; override CMD to serve: docker run ... /app/railsignal -addr :8080
ENTRYPOINT ["/app/railsignal"]
CMD ["--smoke-test"]
