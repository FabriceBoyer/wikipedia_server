ARG GO_VERSION=1.21

FROM golang:${GO_VERSION}-alpine as builder

WORKDIR /app

COPY go.* ./
RUN go mod download

COPY *.go ./
COPY ./wikipedia ./wikipedia
RUN go build -v -o /wikipedia_server

#################################################

# FROM scratch
FROM gcr.io/distroless/static AS final

# ENV DEBIAN_FRONTEND=noninteractive
# RUN apt update && apt install -y pbzip2 && rm -rf /var/lib/apt/lists/*

COPY --from=builder /wikipedia_server /
COPY ./static /static
COPY ./.env.example /.env

CMD [ "/wikipedia_server" ]


