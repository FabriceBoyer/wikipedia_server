FROM golang:1.21-bullseye

ENV DEBIAN_FRONTEND=noninteractive
RUN apt update && apt install -y pbzip2 && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY go.* ./
RUN go mod download

COPY . ./
RUN go build -v -o /wikipedia_server
CMD [ "/wikipedia_server" ]

# CMD ["go", "run", "main.go"]
