FROM golang:1.22.5-alpine AS build
COPY . /src
WORKDIR /src
RUN apk add --no-cache git
RUN go mod download
RUN CGO_ENABLED=0 go build -trimpath .

FROM alpine:latest
RUN apk add --no-cache ca-certificates && mkdir /books
COPY --from=build /src/BookBrowser /
EXPOSE 8090
ENTRYPOINT ["/BookBrowser", "--bookdir", "/books"]
