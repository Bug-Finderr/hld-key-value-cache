FROM golang:1.24.2-alpine

WORKDIR /app

COPY .. ./

RUN go build -o server

EXPOSE 7171

CMD ["./server"]