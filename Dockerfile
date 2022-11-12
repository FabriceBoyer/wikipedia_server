FROM golang:1.19-alpine

# RUN apk add --no-cache pbzip2

WORKDIR /app

COPY go.* ./
RUN go mod download

# Use mounted volume instead of copy for live modifications
# COPY . ./
# RUN go build -v -o /wikipedia_server
# CMD [ "/wikipedia_server" ]

CMD ["go", "run", "main.go"]
