# OlcRTC server Docker image

This image runs `olcrtc` in server mode. The server does not expose an inbound
TCP port; it keeps outbound WebSocket/WebRTC connections to Telemost and relays
client traffic through the room.

## Build

```bash
docker build -t olcrtc/server:local .
```

For Podman:

```bash
podman build -t olcrtc/server:local .
```

## Run

```bash
docker run -d \
  --name olcrtc-server \
  --restart unless-stopped \
  -e OLCRTC_ROOM_ID="your-room-id" \
  -e OLCRTC_KEY="64-hex-character-shared-key" \
  -v olcrtc-state:/var/lib/olcrtc \
  olcrtc/server:local
```

If `OLCRTC_KEY` is omitted, the entrypoint generates a 32-byte key, stores it
in `/var/lib/olcrtc/key.hex`, and prints it once to the logs:

```bash
docker logs olcrtc-server
```

Use the same key on clients.

## Compose

```bash
export OLCRTC_ROOM_ID="your-room-id"
export OLCRTC_KEY="64-hex-character-shared-key"
docker compose -f docker-compose.server.yml up -d --build
```

Optional environment variables:

- `OLCRTC_DNS`: DNS resolver for outbound TCP dials, default `1.1.1.1:53`
- `OLCRTC_DUO`: set to `true` for two parallel WebRTC channels
- `OLCRTC_DEBUG`: set to `true` for verbose logs
- `OLCRTC_KEY_FILE`: persistent key path, default `/var/lib/olcrtc/key.hex`

## Operational notes

- The container runs as a non-root `olcrtc` user.
- The runtime image includes CA certificates for Telemost HTTPS/WSS.
- The healthcheck verifies that the container's PID 1 is the `olcrtc` process.
- No `EXPOSE` is declared because server mode does not accept inbound traffic.
