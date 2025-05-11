FROM golang:alpine AS builder

RUN <<EOF
apk add --no-cache make git &&
    mkdir /clash-config &&
    wget -O /clash-config/Country.mmdb https://raw.githubusercontent.com/Loyalsoldier/geoip/release/Country.mmdb &&
    wget -O /clash-config/geosite.dat https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat &&
    wget -O /clash-config/geoip.dat https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat
EOF

COPY . /clash-src
WORKDIR /clash-src

RUN <<EOF
go mod download &&
    make docker &&
    mv ./bin/clash-docker /clash
EOF

FROM alpine:latest
LABEL org.opencontainers.image.source="https://github.com/Dreamacro/clash"

RUN apk add --no-cache ca-certificates tzdata iptables

VOLUME ["/root/.config/clash/"]

COPY --from=builder /clash-config/ /root/.config/clash/
COPY --from=builder /clash /clash
RUN chmod +x /clash
ENTRYPOINT [ "/clash" ]
