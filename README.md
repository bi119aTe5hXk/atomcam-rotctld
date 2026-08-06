# atomcam-rotctld

`atomcam-rotctld` lets Hamlib and SatNOGS use an ATOM Cam Swing running
[`atomcam_tools`](https://github.com/mnakada/atomcam_tools) as an experimental
azimuth/elevation rotator.

It exposes the Hamlib NET rotctl protocol on TCP port 4533 and translates
position commands into the `atomcam_tools` HTTP command interface. The service
uses only the Go standard library and its Docker image supports ARMv7l hosts
such as Armbian.

## Implemented rotctld commands

| Command | Meaning | Behavior |
| --- | --- | --- |
| `\dump_state` | Hamlib NET rotctl initialization | Reports Az/El, azimuth 0–360°, elevation 0–90° |
| `P az el` | Set position | Queues the newest target and returns immediately |
| `p` | Get position | Returns the last position confirmed by `atomcam_tools` |
| `R 1` | Reset | Runs `moveinit` and queries the resulting position |
| `K` | Park | Queues the configured park position |
| `S` | Stop | Clears a queued target; it cannot physically interrupt a move already running |
| `_` | Get information | Returns the adapter name |

The long command names such as `\set_pos` and `\get_pos` are also accepted.
Continuous manual movement (`M`) is intentionally not implemented.

## Quick start

Copy the example configuration and set the camera IP address:

```sh
cp .env.example .env
```

```dotenv
ATOMCAM_URL=http://192.168.1.80
```

Build and start the independent container:

```sh
docker compose up -d --build
docker compose logs -f atomcam-rotctld
```

Open the Web UI at:

```text
http://HOST_IP:8080/
```

`compose.yaml` uses the published GHCR image by default:

```dotenv
ATOMCAM_ROTCTLD_IMAGE=ghcr.io/bi119ate5hxk/atomcam-rotctld:latest
```

For a local development build, use the build override:

```sh
docker compose -f compose.yaml -f compose.build.yaml up -d --build
```

Test the protocol from another machine or container:

```sh
printf '\\dump_state\n' | nc HOST_IP 4533
printf 'P 120 30\np\n' | nc HOST_IP 4533
```

With Hamlib installed, test the same path used by SatNOGS:

```sh
rotctl -m 2 -r HOST_IP:4533
```

Then enter `P 120 30`, followed by `p`.

## Coordinate calibration

The conversion is deliberately configurable rather than assuming how the
antenna is attached:

```text
camera pan  = PAN_OFFSET + PAN_SCALE × satellite azimuth
camera tilt = TILT_HORIZON + TILT_SCALE × satellite elevation
```

Default initial values assume the camera's front-facing raw pose, pan 180 and
tilt 90, points north. For a top-mounted antenna, elevation tracking uses
tilt 0 at the horizon and tilt 90 at zenith:

```dotenv
PAN_OFFSET=180
PAN_SCALE=1
TILT_HORIZON=0
TILT_SCALE=1
```

This maps satellite azimuth 0° and elevation 0° to camera pan 180 and tilt 0.
It maps satellite elevation 90° to camera tilt 90. Set `TILT_SCALE=-1` and
adjust `TILT_HORIZON` only if the mechanism or antenna mount is reversed.

`PAN_OFFSET` is the raw pan value that points north. Keep the default when the
camera's front-facing raw pose, pan 180 / tilt 90, is physically aimed north. If
the mounted camera is rotated, change `PAN_OFFSET` until a logical azimuth 0°
command points the antenna's active broadside direction north.

Although the product is described as a 360° camera, the `atomcam_tools` command
range is 0–355°. Targets in the remaining five-degree command gap are mapped to
the nearest endpoint. Place that gap in an unimportant direction using
`PAN_OFFSET` if necessary.

## SatNOGS Client configuration

Keep the existing radio settings unchanged:

```dotenv
SATNOGS_RIG_ENABLED=true
SATNOGS_RIG_IP=rigctld
SATNOGS_RIG_PORT=4532
```

`SATNOGS_RIG_IP` is for radio frequency control and is independent of the
rotator. Enable the rotator with:

```dotenv
SATNOGS_ROT_ENABLED=true
SATNOGS_ROT_MODEL=ROT_MODEL_NETROTCTL
SATNOGS_ROT_PORT=HOST_IP:4533
SATNOGS_ROT_THRESHOLD=3
```

Because port 4533 is published by `compose.yaml`, `HOST_IP` can be the LAN IP
of the host. Do not use `127.0.0.1`: inside the SatNOGS container that
means the SatNOGS container itself.

If both containers share a Docker network, do not publish the port and use the
service name instead:

```dotenv
SATNOGS_ROT_PORT=atomcam-rotctld:4533
```

The separate `librespace/hamlib` container running `rigctld` on port 4532 can
continue operating normally.

In Docker Compose this usually means setting the same values directly in the
`satnogs_client.environment` block, because Compose `environment` entries can
override values from `env_file`:

```yaml
environment:
  SATNOGS_RIG_ENABLED: "true"
  SATNOGS_RIG_IP: rigctld
  SATNOGS_RIG_PORT: "4532"
  SATNOGS_ROT_ENABLED: "true"
  SATNOGS_ROT_MODEL: ROT_MODEL_NETROTCTL
  SATNOGS_ROT_PORT: 192.168.1.100:4533
  SATNOGS_ROT_THRESHOLD: "3"
```

If the runtime log shows `network_open: failed to connect to 127.0.0.1:4532`,
the failing connection is rig control, not rotator control. Check the live
container with:

```sh
docker exec satnogs-client env | grep '^SATNOGS_RIG'
docker exec satnogs-client getent hosts rigctld
```

## Web UI

The built-in Web UI listens on port 8080 by default:

```dotenv
WEB_LISTEN_ADDRESS=0.0.0.0:8080
MANUAL_STEP=5
```

It shows the current azimuth and elevation as both numbers and a polar plot,
the ATOM Cam URL, rotctld listen address, and runtime state. The state is
`STANDBY` when no Hamlib tracking session is active and `TRACKING` while a
rotctld client session has opened capabilities with `\dump_state`. `RESETTING`
and `MOVING` are shown while those operations are active.

The directional buttons adjust logical azimuth/elevation by `MANUAL_STEP`
degrees. Left/right change azimuth; up/down change elevation. `Park` queues the
configured park position. `Reset Position` runs the same reset path as Hamlib
`R 1`.

## Reset and movement behavior

ATOM Cam Swing has no independent absolute angle sensor. Its reported position
is the motor controller's estimated position. `R 1` calls the `atomcam_tools`
`moveinit` routine, which drives both axes to their endpoints and restores its
coordinate system.

The camera normally calibrates when it boots, so `RESET_ON_START` defaults to
`false`. Set it to `true` only when every proxy restart should also recalibrate
the camera.

Set `RESET_ON_SESSION=true` to start a reset when Hamlib opens a rotator
session, which is normally when SatNOGS begins using the rotator for an
observation. The reset runs asynchronously so Hamlib initialization can return
quickly; movement commands wait for the reset to finish and then the newest
target is executed.

By default, after a reset the adapter sends the camera to the reference north
pose:

```dotenv
RESET_ON_SESSION=true
RESET_CAMERA_PAN=180
RESET_CAMERA_TILT=90
```

This assumes raw pan 180 / tilt 90 is the camera's front-facing pose. If your
camera reports different raw coordinates for that pose, set both values to your
measured atomcam_tools coordinates. Leave both values at `-1` to disable the
extra post-reset move.

Only one ATOM Cam move can run at a time. While it is moving, the adapter keeps
only the newest SatNOGS target. Intermediate targets are discarded. It also
applies `MOVE_THRESHOLD`, using circular distance around azimuth north, to
reduce motor wear.

## ARMv7 image

On the ARMv7 host, a normal local build is enough:

```sh
docker compose build
```

If the build fails at `COPY cmd ./cmd` or `COPY internal ./internal`, the source
tree copied to the host is incomplete. The Docker build context must
contain at least:

```text
cmd/
internal/
go.mod
Dockerfile
compose.yaml
.env
```

Create a complete source archive on the development machine with:

```sh
make package
```

Then copy `dist/atomcam-rotctld-src.tar.gz` to the host, extract it into
the deployment directory, and run:

```sh
docker compose up -d --build --force-recreate
```

To cross-build an ARMv7 image on another architecture:

```sh
docker buildx build --platform linux/arm/v7 --load \
  -t atomcam-rotctld:armv7 .
```

## Important limitations

- `S` cannot stop a physical movement already accepted by the camera because
  `atomcam_tools` exposes absolute move but no immediate motor-stop command.
- `p` reports the last camera-confirmed estimate, not encoder feedback.
- A successful `P` means the target was accepted into the adapter queue. A
  later camera/network error is logged and the confirmed position is not
  updated.
- Camera authentication is not yet implemented. Use this only on a trusted LAN
  and do not expose port 4533 or the camera Web UI to the internet.
- GP and dipole elements have a null along the element axis. Calibrate against
  the antenna's broadside direction rather than assuming the element itself
  should point at the satellite.

## Development

```sh
go test ./...
go test -race ./...
```
