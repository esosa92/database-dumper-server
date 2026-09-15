FROM golang:1.26 AS build
WORKDIR /build
COPY src/go.mod src/go.sum ./
RUN go mod download
COPY src/ .
RUN CGO_ENABLED=0 go build -o /database-dumper .

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends openssh-client sshpass ca-certificates \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /data
COPY --from=build /database-dumper /database-dumper
COPY deploy/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENV DUMPER_DATA_DIR=/data
ENV DUMPER_LISTEN=:8080
EXPOSE 8080
ENTRYPOINT ["/entrypoint.sh"]
CMD ["/database-dumper"]
