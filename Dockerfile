FROM golang:1.23

WORKDIR /app

COPY . .
RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux go build -o /event-relay main.go

EXPOSE 8080

CMD ["/event-relay"]