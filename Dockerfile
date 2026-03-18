FROM golang:1.25-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

COPY go.mod go.sum ./
COPY pkg/apis/go.mod pkg/apis/go.sum pkg/apis/
RUN go mod download

COPY cmd/ cmd/
COPY pkg/ pkg/

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-extldflags=-static" -o /cozystack-scheduler ./cmd/cozystack-scheduler/

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /cozystack-scheduler /cozystack-scheduler
ENTRYPOINT ["/cozystack-scheduler"]
