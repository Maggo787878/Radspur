FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /radspur-server .

FROM alpine:3.20
RUN adduser -D -u 10001 radspur && mkdir /data && chown radspur /data
USER radspur
COPY --from=build /radspur-server /usr/local/bin/radspur-server
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["radspur-server"]
