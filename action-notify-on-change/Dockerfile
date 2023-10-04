FROM golang:1.21-alpine as build

WORKDIR /app

COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -tags netgo -ldflags '-w' -o /action-notify-on-change ./*.go

FROM scratch
COPY --from=build /action-notify-on-change /action-notify-on-change
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ENTRYPOINT ["/action-notify-on-change"]
