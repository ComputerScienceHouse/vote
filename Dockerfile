FROM docker.io/golang:1.24-alpine AS build

WORKDIR /src/
RUN apk add git
COPY go* .
COPY *.go .
COPY database database
COPY logging logging
COPY sse sse
RUN go build -v -o vote

FROM docker.io/alpine
RUN apk add --no-cache tzdata
ENV TZ=America/New_York
RUN cp /usr/share/zoneinfo/America/New_York /etc/localtime
COPY static /static
COPY templates /templates
COPY --from=build /src/vote /vote

ENTRYPOINT [ "/vote" ]
