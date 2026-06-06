FROM golang:1.22-alpine AS build
WORKDIR /app
RUN apk add --no-cache ca-certificates git

# Copy module file first so Docker can cache dependency downloads.
# go.sum is generated in the build layer when the repository does not have it yet.
COPY go.mod ./
RUN go mod download || true

COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -mod=mod -trimpath -ldflags="-s -w" -o /checkinme-api ./cmd/api

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -H -u 10001 appuser
WORKDIR /app
COPY --from=build /checkinme-api /app/checkinme-api
USER appuser
EXPOSE 8080
CMD ["/app/checkinme-api"]
