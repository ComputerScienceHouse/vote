FROM docker.io/golang:1.18.4-alpine3.16 AS build

WORKDIR /src/
RUN apk add git
COPY go* .
COPY main.go .
COPY database database
COPY logging logging
COPY sse sse
RUN go build -v -o vote

FROM docker.io/alpine:3.16
COPY static /static
COPY templates /templates
COPY --from=build /src/vote /vote

ENTRYPOINT [ "/vote" ]
