## HTTP/3 powered transparent proxy

![what is this](./what-is-this.png)

It takes advantage of [Caddy (v2)](https://caddyserver.com/).

```bash
make build

./bin/goodog-backend-caddy start

# Edit <XXX> in ./etc/caddy.json
curl localhost:2019/load -X POST -H "Content-Type: application/json" -d @etc/caddy.json

./bin/goodog-frontend -server-uri https://<DOMAIN>/?version=v1&compression=snappy -listen :2020
```
