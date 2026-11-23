FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG PRE_RELEASE=""
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/Bnei-Baruch/archive-stt/version.PreRelease=${PRE_RELEASE}" -o /archive-stt .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /archive-stt /archive-stt
# the data dir is a volume; its owner on the host must match the user the container runs as (compose: user)
ENV ARCHIVE_STT_DATA_DIR=/data ARCHIVE_STT_SERVER_BIND_ADDRESS=:8770
VOLUME /data
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["/archive-stt", "health"]
ENTRYPOINT ["/archive-stt"]
CMD ["server"]
