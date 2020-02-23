FROM golang:alpine AS builder
RUN apk add --no-cache ca-certificates \
        make \
        git
COPY . /goodog
RUN cd /goodog && make build


FROM alpine AS goodog-frontend
RUN apk add --no-cache tzdata
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs
COPY --from=builder /goodog/bin/goodog-frontend /usr/bin/goodog-frontend
ARG TZ
ENV TZ $TZ
ENV GOODOG_SERVER_URI :invalid:
ENV GOODOG_LISTEN_ADDRESS :59487
ENV GOODOG_CONNECTOR caddy-http3
ENV GOODOG_TIMEOUT 30s
ENV GOODOG_LOG_LEVEL info
EXPOSE 59487/tcp
EXPOSE 59487/udp
CMD ["sh", "-c", \
     "goodog-frontend \
     -server ${GOODOG_SERVER_URI:=:invalid:} \
     -listen ${GOODOG_LISTEN_ADDRESS:=:59487} \
     -connector ${GOODOG_CONNECTOR:=caddy-http3} \
     -timeout ${GOODOG_TIMEOUT:=5s} \
     -pprof-addr ${GOODOG_PPROF_ADDR} \
     -log-level ${GOODOG_LOG_LEVEL:=info}"]


FROM alpine AS goodog-backend-caddy
RUN apk add --no-cache tzdata
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs
COPY --from=builder /goodog/bin/goodog-backend-caddy /usr/bin/goodog-backend-caddy
ARG TZ
ENV TZ $TZ
EXPOSE 80
EXPOSE 443/tcp
EXPOSE 443/udp
EXPOSE 2019
ENTRYPOINT ["goodog-backend-caddy"]
CMD ["run"]
