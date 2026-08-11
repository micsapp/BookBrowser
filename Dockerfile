FROM golang:1.22.5-alpine AS build
COPY . /src
WORKDIR /src
RUN apk add --no-cache git
RUN go mod download
RUN mkdir -p /out && \
    CGO_ENABLED=0 go build -trimpath -o /out/BookBrowser . && \
    CGO_ENABLED=0 go build -trimpath -o /out/bookbrowser-cli ./cmd/bookbrowser-cli

FROM alpine:latest
RUN apk add --no-cache ca-certificates && mkdir /books
COPY --from=build /out/BookBrowser /BookBrowser
COPY --from=build /out/bookbrowser-cli /usr/local/bin/bookbrowser-cli
EXPOSE 8090
ENTRYPOINT ["/BookBrowser", "--bookdir", "/books"]
