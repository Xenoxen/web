FROM node:23.6-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN yarn
COPY frontend/ ./
RUN yarn build

# After build, move the output to /app/static
RUN mkdir -p /app/static && cp -r build /app/static

FROM golang:1.25.5-alpine AS backend-builder
WORKDIR /go/pkg/ocap

COPY cmd/ ./cmd
COPY server/ ./server
COPY go.mod .
COPY go.sum ./

ARG build_commit
RUN apk add --no-cache alpine-sdk && go build -ldflags "-X github.com/OCAP2/web/server.BuildDate=`date -u +'%Y-%m-%dT%H:%M:%SZ'` -X github.com/OCAP2/web/server.BuildCommit=$build_commit" -a -o app ./cmd

FROM alpine:3.14
WORKDIR /usr/local/ocap
RUN mkdir -p /etc/ocap /usr/local/ocap/data /var/lib/ocap/db /var/lib/ocap/maps /var/lib/ocap/data && \
    echo '{}' > /etc/ocap/setting.json

ENV OCAP_MARKERS=/usr/local/ocap/markers
ENV OCAP_AMMO=/usr/local/ocap/ammo
ENV OCAP_STATIC=/usr/local/ocap/static

ENV OCAP_DB=/var/lib/ocap/db/data.db
ENV OCAP_MAPS=/var/lib/ocap/maps
ENV OCAP_DATA=/var/lib/ocap/data

ENV OCAP_LISTEN=0.0.0.0:5000
EXPOSE 5000/tcp

COPY markers /usr/local/ocap/markers
COPY ammo /usr/local/ocap/ammo

# Copy built frontend from frontend-builder stage
COPY --from=frontend-builder /app/static/build /usr/local/ocap/static

# For development only
COPY data /var/lib/ocap/data
COPY data.db /var/lib/ocap/db/data.db

COPY --from=backend-builder /go/pkg/ocap/app /usr/local/ocap/app

CMD ["/usr/local/ocap/app"]