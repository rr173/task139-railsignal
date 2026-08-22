# Benzhi evaluation Dockerfile for task139-railsignal.
# Native HTML/CSS/JS frontend embedded via //go:embed — no Node build needed,
# so this uses the pure-Go template A.
FROM golang:1.26.3

ENV GOTOOLCHAIN=local \
    GOPROXY=https://goproxy.cn,direct \
    GOSUMDB=sum.golang.google.cn

WORKDIR /app

# Go dependencies
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Pre-compile to leave build cache in the image
RUN go build ./...

CMD ["bash"]
