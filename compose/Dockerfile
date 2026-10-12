FROM golang:1.27.1-alpine3.24 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
COPY internal ./internal

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tabletop .
RUN mkdir -p /runtime/data /runtime/tmp && \
    chown 10001:10001 /runtime/data && chmod 1777 /runtime/tmp

FROM scratch
WORKDIR /app
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=10001:10001 /out/tabletop /app/tabletop
COPY --from=build --chown=10001:10001 /runtime/data /app/data
COPY --from=build /runtime/tmp /tmp
COPY static ./static
COPY templates ./templates
COPY index.html ./index.html

USER 10001:10001
EXPOSE 18080
VOLUME ["/app/data"]
ENTRYPOINT ["/app/tabletop"]
