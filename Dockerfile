FROM golang:1.22-alpine AS build
WORKDIR /app
RUN apk add --no-cache ca-certificates
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /checkinme-api ./cmd/api

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /checkinme-api /app/checkinme-api
EXPOSE 8080
CMD ["/app/checkinme-api"]
