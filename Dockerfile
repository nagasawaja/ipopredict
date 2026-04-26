FROM golang:1.24-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o /out/hkipo ./cmd/hkipo

FROM debian:bookworm-slim

RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates tzdata \
	&& rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=build /out/hkipo /app/hkipo

ENV HK_IPO_DB_PATH=/app/sql/hk_ipo.db
ENV TZ=Asia/Shanghai

EXPOSE 8083

CMD ["/app/hkipo", "web", "-addr", ":8083"]
