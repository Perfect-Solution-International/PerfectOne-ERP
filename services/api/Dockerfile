FROM golang:1.23-alpine AS build
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN go test ./...
RUN CGO_ENABLED=0 go build -o /grocerly-api .
FROM alpine:3.21
RUN apk add --no-cache postgresql16-client ca-certificates
COPY --from=build /grocerly-api /grocerly-api
EXPOSE 8080
CMD ["/grocerly-api"]
