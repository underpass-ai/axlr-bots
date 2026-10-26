# ArduPilot SITL for the port fleet: ArduCopter (quad X), ArduRover and
# ArduPlane 4.7.2, the official static SITL builds from firmware.ardupilot.org
# (all three at commit fb77356), the default parameters sim_vehicle.py uses at
# that tag, and mavlink-router to share each vehicle's MAVLink.
FROM docker.io/library/ubuntu:24.04@sha256:a2d43bffc46670d7108bb3c0468c67c5557c4c92e83e1e34cbff1322ad1b94dc AS build

ARG FW=https://firmware.ardupilot.org
ARG RAW=https://raw.githubusercontent.com/ArduPilot/ardupilot/Copter-4.7.2/Tools/autotest
ARG MAVLINK_ROUTER_REF=v4
ARG MAVLINK_ROUTER_COMMIT=42529d55b665e0a9a29e424e186f514c56c2e5b5

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl git g++ meson ninja-build pkg-config python3 \
 && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /opt/sitl && cd /opt/sitl \
 && curl -fsSL -o arducopter "$FW/Copter/stable-4.7.2/SITL_x86_64_linux_gnu/arducopter" \
 && curl -fsSL -o ardurover  "$FW/Rover/stable-4.7.2/SITL_x86_64_linux_gnu/ardurover" \
 && curl -fsSL -o arduplane  "$FW/Plane/stable-4.7.2/SITL_x86_64_linux_gnu/arduplane" \
 && curl -fsSL -o copter.parm   "$RAW/default_params/copter.parm" \
 && curl -fsSL -o copter-X.parm "$RAW/default_params/copter-X.parm" \
 && curl -fsSL -o rover.parm    "$RAW/default_params/rover.parm" \
 && curl -fsSL -o plane.parm    "$RAW/models/plane.parm" \
 && printf '%s\n' \
    "caed35e4cb796ec06cac40b09566a18251771486b1cbac5e2bf16b7a5ed5045d  arducopter" \
    "f453fd8217b9d798c873e5c424e139a2a3597b52e31658f76b2c177b523391af  ardurover" \
    "6867c162941c6f5ed3e06db667561aa093413e186cbd5511ea444fc6854168f1  arduplane" \
    "5e01345b45d1c6190b28bece5638bbdd4cf1cce35e05bbbf480ab24d2b51aa0e  copter.parm" \
    "cf4ac873fb3472edad53ec6550a329cd704c02748b959bfbb2ce37cfe856e236  copter-X.parm" \
    "6479879d3e625854a6c32cb4d3fe231bab81d935e07904eac8a411fd6f971bc4  rover.parm" \
    "93ba9a70c771609a90b81249d6a1d5a9df8d48bef7d149b42b2d9c7fbd06494a  plane.parm" \
    | sha256sum -c - \
 && chmod 0755 arducopter ardurover arduplane

RUN git clone --depth 1 --branch "$MAVLINK_ROUTER_REF" --recurse-submodules --shallow-submodules \
      https://github.com/mavlink-router/mavlink-router /src/mavlink-router \
 && cd /src/mavlink-router \
 && test "$(git rev-parse HEAD)" = "$MAVLINK_ROUTER_COMMIT" \
 && meson setup build . --buildtype=release -Dsystemdsystemunitdir=/usr/lib/systemd/system \
 && ninja -C build

FROM docker.io/library/ubuntu:24.04@sha256:a2d43bffc46670d7108bb3c0468c67c5557c4c92e83e1e34cbff1322ad1b94dc
COPY --from=build /opt/sitl /opt/sitl
COPY --from=build /src/mavlink-router/build/src/mavlink-routerd /usr/local/bin/mavlink-routerd
COPY sitl-run /usr/local/bin/sitl-run
# uid 1000 (ubuntu) exists in the base image; SITL writes eeprom.bin and logs/ in /data.
RUN mkdir -p /data && chown 1000:1000 /data
USER 1000
WORKDIR /data
ENTRYPOINT ["/usr/local/bin/sitl-run"]
