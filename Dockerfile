FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY go.sum ./
COPY *.go ./
COPY web ./web
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/newapi-upload .

FROM alpine:3.23
RUN addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=builder /out/newapi-upload /app/newapi-upload
RUN chown -R app:app /app
USER app
EXPOSE 8080
ENV PORT=8080
ENTRYPOINT ["/app/newapi-upload"]
