FROM golang:1.21-alpine AS build

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download && apk update && apk add gcc musl-dev

COPY *.go ./

RUN CGO_ENABLED=1 go build -o /app/app

## Deploy
FROM alpine

COPY --from=build /app/app /app
RUN apk upgrade --no-cache && apk add --no-cache bash jq sqlite vim \
    && chmod +x /app
EXPOSE 4000
ENTRYPOINT ["/app"]
