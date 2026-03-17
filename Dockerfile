FROM golang:1.23-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ENV CGO_ENABLED=1
RUN go build -o /jorm ./cmd/jorm

# Runtime image with Claude Code and dev tools
FROM node:22-alpine

RUN apk add --no-cache git github-cli bash sqlite
RUN npm install -g @anthropic-ai/claude-code

COPY --from=builder /jorm /usr/local/bin/jorm

WORKDIR /workspace
ENTRYPOINT ["jorm"]
