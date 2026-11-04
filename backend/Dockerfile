FROM alpine:3.19

WORKDIR /app

RUN mkdir -p /app/static/uploads/products

COPY bin/server-linux /app/server
COPY migrations /app/migrations

RUN chmod +x /app/server

EXPOSE 8080

CMD ["/app/server"]

