## HTTP/3 powered transparent proxy

![what is this](./what-is-this.png)

It takes advantage of [Caddy (v2)](https://caddyserver.com/).

```bash
make build

# Start backend
./bin/goodog-backend-caddy start

# Generate a hashed password
./bin/goodog-backend-caddy hash-password -algorithm bcrypt -plaintext PASSWORD # -salt "SALT"
# Edit <PLACEHOLDER> in ./etc/caddy.json
curl localhost:2019/load -X POST -H "Content-Type: application/json" -d @etc/caddy.json

# Start frontend
# The full server uri: https://USERNAME:PASSWORD@DOMAIN/?version=v1&compression=snappy
./bin/goodog-frontend -server https://USERNAME:PASSWORD@DOMAIN/?version=v1 -listen :59487
```
