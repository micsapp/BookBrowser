FROM golang:1.22.5-alpine AS build
COPY . /src
WORKDIR /src
RUN apk add --no-cache git
RUN go mod download
RUN mkdir -p /out && \
    CGO_ENABLED=0 go build -trimpath -o /out/BookBrowser . && \
    CGO_ENABLED=0 go build -trimpath -o /out/bookbrowser-cli ./cmd/bookbrowser-cli

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends calibre ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir /books
COPY --from=build /out/BookBrowser /BookBrowser
COPY --from=build /out/bookbrowser-cli /usr/local/bin/bookbrowser-cli
EXPOSE 8090
ENTRYPOINT ["/BookBrowser", "--bookdir", "/books"]
