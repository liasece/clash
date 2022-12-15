<h1 align="center">
  <img src="https://github.com/Dreamacro/clash/raw/master/docs/logo.png" alt="Clash" width="200">
  <br>Clash<br>
</h1>

<h4 align="center">A rule-based tunnel in Go.</h4>

<p align="center">
  <a href="https://github.com/Dreamacro/clash/actions">
    <img src="https://img.shields.io/github/workflow/status/Dreamacro/clash/Go?style=flat-square" alt="Github Actions">
  </a>
  <a href="https://goreportcard.com/report/github.com/Dreamacro/clash">
    <img src="https://goreportcard.com/badge/github.com/Dreamacro/clash?style=flat-square">
  </a>
  <img src="https://img.shields.io/github/go-mod/go-version/Dreamacro/clash?style=flat-square">
  <a href="https://github.com/Dreamacro/clash/releases">
    <img src="https://img.shields.io/github/release/Dreamacro/clash/all.svg?style=flat-square">
  </a>
  <a href="https://github.com/Dreamacro/clash/releases/tag/premium">
    <img src="https://img.shields.io/badge/release-Premium-00b4f0?style=flat-square">
  </a>
</p>

## Features

- Local HTTP/HTTPS/SOCKS server with authentication support
- VMess, Shadowsocks, Trojan, Snell protocol support for remote connections
- Built-in DNS server that aims to minimize DNS pollution attack impact, supports DoH/DoT upstream and fake IP.
- Rules based off domains, GEOIP, IPCIDR or Process to forward packets to different nodes
- Remote groups allow users to implement powerful rules. Supports automatic fallback, load balancing or auto select node based off latency
- Remote providers, allowing users to get node lists remotely instead of hardcoding in config
- Netfilter TCP redirecting. Deploy Clash on your Internet gateway with `iptables`.
- Comprehensive HTTP RESTful API controller

## Getting Started
Documentations are now moved to [GitHub Wiki](https://github.com/Dreamacro/clash/wiki).

## Advanced usage for this branch

## Build

You should install [golang](https://go.dev) first.

If you can't visit github,you should set proxy first:
```shell
go env -w GOPROXY=https://goproxy.io,direct
```

So now you can build it:
```shell
go build
```

### DNS configuration

Support `geosite` with `fallback-filter`.

Restore `Redir remote resolution`.

Support resolve ip with a `Proxy Tunnel`.

```yaml
proxy-groups:

  - name: DNS
    type: url-test
    use:
      - HK
    url: http://cp.cloudflare.com
    interval: 180
    lazy: true
```
```yaml
dns:
  enable: true
  use-hosts: true
  ipv6: false
  enhanced-mode: redir-host
  fake-ip-range: 198.18.0.1/16
  listen: 127.0.0.1:6868
  default-nameserver:
    - 119.29.29.29
    - 114.114.114.114
  nameserver:
    - https://doh.pub/dns-query
    - tls://223.5.5.5:853
  fallback:
    - 'https://1.0.0.1/dns-query#DNS'  # append the proxy adapter name or group name to the end of DNS URL with '#' prefix.
    - 'tls://8.8.4.4:853#DNS'
  fallback-filter:
    geoip: false
    geosite:
      - gfw  # `geosite` filter only use fallback server to resolve ip, prevent DNS leaks to unsafe DNS providers.
    domain:
      - +.example.com
    ipcidr:
      - 0.0.0.0/32
```

### TUN configuration

Supports macOS, Linux and Windows.

Built-in [Wintun](https://www.wintun.net) driver.

```yaml
# Enable the TUN listener
tun:
  enable: true
  stack: gvisor #  only gvisor
  dns-hijack: 
    - 0.0.0.0:53 # additional dns server listen on TUN
  auto-route: true # auto set global route
```
### Rules configuration
- Support rule `GEOSITE`.
- Support rule-providers `RULE-SET`.
- Support `multiport` condition for rule `SRC-PORT` and `DST-PORT`.
- Support `network` condition for all rules.
- Support source IPCIDR condition for all rules, just append to the end.
- The `GEOSITE` databases via https://github.com/Loyalsoldier/v2ray-rules-dat.
```yaml
rules:

  # network(tcp/udp) condition for all rules
  - DOMAIN-SUFFIX,bilibili.com,DIRECT,tcp
  - DOMAIN-SUFFIX,bilibili.com,REJECT,udp
    
  # multiport condition for rules SRC-PORT and DST-PORT
  - DST-PORT,123/136/137-139,DIRECT,udp
  
  # rule GEOSITE
  - GEOSITE,category-ads-all,REJECT
  - GEOSITE,icloud@cn,DIRECT
  - GEOSITE,apple@cn,DIRECT
  - GEOSITE,apple-cn,DIRECT
  - GEOSITE,microsoft@cn,DIRECT
  - GEOSITE,facebook,PROXY
  - GEOSITE,youtube,PROXY
  - GEOSITE,geolocation-cn,DIRECT
  - GEOSITE,geolocation-!cn,PROXY
    
  # source IPCIDR condition for all rules in gateway proxy
  #- GEOSITE,geolocation-!cn,REJECT,192.168.1.88/32,192.168.1.99/32

  - GEOIP,telegram,PROXY,no-resolve
  - GEOIP,private,DIRECT,no-resolve
  - GEOIP,cn,DIRECT
  
  - MATCH,PROXY
```


### Proxies configuration

Active health detection `urltest / fallback` (based on tcp handshake, multiple failures within a limited time will actively trigger health detection to use the node)

Support `Policy Group Filter`

```yaml
proxy-groups:

  - name: 🚀 HK Group
    type: select
    use:
      - ALL
    filter: 'HK'

  - name: 🚀 US Group
    type: select
    use:
      - ALL
    filter: 'US'

proxy-providers:
  ALL:
    type: http
    url: "xxxxx"
    interval: 3600
    path: "xxxxx"
    health-check:
      enable: true
      interval: 600
      url: http://www.gstatic.com/generate_204

```



Support outbound transport protocol `VLESS`.

The XTLS support (TCP/UDP) transport by the XRAY-CORE.
```yaml
proxies:
  - name: "vless"
    type: vless
    server: server
    port: 443
    uuid: uuid
    servername: example.com # AKA SNI
    # flow: xtls-rprx-direct # xtls-rprx-origin  # enable XTLS
    # skip-cert-verify: true
    
  - name: "vless-ws"
    type: vless
    server: server
    port: 443
    uuid: uuid
    tls: true
    udp: true
    network: ws
    servername: example.com # priority over wss host
    # skip-cert-verify: true
    ws-opts:
      path: /path
      headers: { Host: example.com, Edge: "12a00c4.fm.huawei.com:82897" }

  - name: "vless-grpc"
    type: vless
    server: server
    port: 443
    uuid: uuid
    tls: true
    udp: true
    network: grpc
    servername: example.com # priority over wss host
    # skip-cert-verify: true
    grpc-opts: 
      grpc-service-name: grpcname
```

### IPTABLES configuration
Work on Linux OS who's supported `iptables`

```yaml
# Enable the TPROXY listener
tproxy-port: 9898

iptables:
  enable: true # default is false
  inbound-interface: eth0 # detect the inbound interface, default is 'lo'
```

### proxy server connect pool configuration
faster connect to remote proxy server(shadowsocks)

```yaml
# Enable proxy server connect pool, try to prepare 5 links for the upcoming use
connect-pool-size: 5
```

## Premium

Premium core is proprietary. You can find their release notes and pre-built binaries [here](https://github.com/Dreamacro/clash/releases/tag/premium).

- gvisor/system stack TUN device on macOS, Linux and Windows ([ref](https://github.com/Dreamacro/clash/wiki/Clash-Premium-Features#tun-device))
- Policy routing with [Scripts](https://github.com/Dreamacro/clash/wiki/Clash-Premium-Features#script)
- Load your rules with [Rule Providers](https://github.com/Dreamacro/clash/wiki/Clash-Premium-Features#rule-providers)
- Monitor Clash usage with a built-in profiling engine. ([Dreamacro/clash-tracing](https://github.com/Dreamacro/clash-tracing))

## Getting Started
Documentations are available at [GitHub Wiki](https://github.com/Dreamacro/clash/wiki).

## Development
If you want to build a Go application that uses Clash as a library, check out the [GitHub Wiki](https://github.com/Dreamacro/clash/wiki/Using-Clash-in-your-Golang-program).

## Credits

* [Dreamacro/clash](https://github.com/Dreamacro/clash)
* [riobard/go-shadowsocks2](https://github.com/riobard/go-shadowsocks2)
* [v2ray/v2ray-core](https://github.com/v2ray/v2ray-core)
* [WireGuard/wireguard-go](https://github.com/WireGuard/wireguard-go)
* [yaling888/clash-plus-pro](https://github.com/yaling888/clash)

## License

This software is released under the GPL-3.0 license.

[![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2FDreamacro%2Fclash.svg?type=large)](https://app.fossa.io/projects/git%2Bgithub.com%2FDreamacro%2Fclash?ref=badge_large)
