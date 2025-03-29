FROM golang:1.24.2-alpine AS builder
WORKDIR /app
COPY go.mod main.go ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o server -tags=netgo

FROM scratch
COPY --from=builder /app/server /server
ENV GOMAXPROCS=2 GOGC=40 GODEBUG=madvdontneed=1,gctrace=0

EXPOSE 7171
CMD ["/server"]