# fleet-gz: the port fleet's one gz sim world. gz sim Harmonic with
# ArduPilot's gz plugin, the Maasvlakte 2 world, the fleet's vehicle models
# (templates fleet-sim fills in), the scripts of the pod fleet-gz (fleet-sim,
# fpv-server.py, ais-bridge.py) and, for the benches, the ArduCopter and
# ArduRover 4.7.2 SITL the fleet runs.
#
#   podman build -t localhost/fleet-gz:0.3.0 examples/drone-fleet/gz
FROM docker.io/osrf/ros:jazzy-desktop-full@sha256:ae7ad3ac243da1dfd8bd402a2fa08e149ffbf7a385013bd8ef798d0800accdc8

ARG FW=https://firmware.ardupilot.org
ARG RAW=https://raw.githubusercontent.com/ArduPilot/ardupilot/Copter-4.7.2/Tools/autotest
ARG ARDUPILOT_GAZEBO_COMMIT=80dc9664dda7efd6c5d1a71a79198d1d6b708c0e

RUN apt-get update \
 && apt-get install -y --no-install-recommends rapidjson-dev libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev python3-pip \
 && rm -rf /var/lib/apt/lists/*

# The plugin that couples a gz model to ArduPilot's JSON SITL backend, built
# against the gz Harmonic of the ROS 2 Jazzy vendor packages. Two patches:
# attitude sends the Euler angles beside the quaternion (ArduPilot 4.7.2
# drops rng_1 without them; master tests the right bits); nonblocking stops
# the plugin waiting for a servo packet on every step when lock_step is off
# (1 ms without a SITL, up to 10 ms with one, per vehicle and in series: one
# quad ran at 0.40 x real time, six at 0.06).
COPY ardupilot_gazebo-attitude.patch ardupilot_gazebo-nonblocking.patch /src/
RUN git clone https://github.com/ArduPilot/ardupilot_gazebo /src/ardupilot_gazebo \
 && cd /src/ardupilot_gazebo \
 && git checkout -q "$ARDUPILOT_GAZEBO_COMMIT" \
 && git apply /src/ardupilot_gazebo-attitude.patch /src/ardupilot_gazebo-nonblocking.patch \
 && . /opt/ros/jazzy/setup.sh \
 && GZ_VERSION=harmonic cmake -B build -DCMAKE_BUILD_TYPE=RelWithDebInfo -DCMAKE_INSTALL_PREFIX=/opt/ardupilot_gazebo \
 && cmake --build build -j"$(nproc)" \
 && cmake --install build \
 && rm -rf /src/ardupilot_gazebo

# The same ArduCopter and ArduRover binaries and default parameters as
# ../sitl/Dockerfile, for the benches (rover-sim, quad_test.py). The fleet's
# vehicles run their SITL in their own pods.
RUN mkdir -p /opt/sitl && cd /opt/sitl \
 && curl -fsSL -o arducopter "$FW/Copter/stable-4.7.2/SITL_x86_64_linux_gnu/arducopter" \
 && curl -fsSL -o ardurover "$FW/Rover/stable-4.7.2/SITL_x86_64_linux_gnu/ardurover" \
 && curl -fsSL -o copter.parm   "$RAW/default_params/copter.parm" \
 && curl -fsSL -o copter-X.parm "$RAW/default_params/copter-X.parm" \
 && curl -fsSL -o rover.parm "$RAW/default_params/rover.parm" \
 && printf '%s\n' \
    "caed35e4cb796ec06cac40b09566a18251771486b1cbac5e2bf16b7a5ed5045d  arducopter" \
    "f453fd8217b9d798c873e5c424e139a2a3597b52e31658f76b2c177b523391af  ardurover" \
    "5e01345b45d1c6190b28bece5638bbdd4cf1cce35e05bbbf480ab24d2b51aa0e  copter.parm" \
    "cf4ac873fb3472edad53ec6550a329cd704c02748b959bfbb2ce37cfe856e236  copter-X.parm" \
    "6479879d3e625854a6c32cb4d3fe231bab81d935e07904eac8a411fd6f971bc4  rover.parm" \
    | sha256sum -c - \
 && chmod 0755 arducopter ardurover

RUN pip install --no-cache-dir --break-system-packages pymavlink==2.4.50
RUN pip install --no-cache-dir --break-system-packages pillow==12.0.0

# The fleet's world and models, and the scripts. port_iris is the plugin's
# iris with a box body and sphere feet (port_iris.py): what the contact
# solver can keep up with for six vehicles.
COPY models /gz/models
COPY port_iris.py /gz/
RUN python3 /gz/port_iris.py /opt/ardupilot_gazebo/share/ardupilot_gazebo/models /gz/models
COPY worlds /gz/worlds
COPY fleet-sim fpv-server.py ais-bridge.py quad_test.py vehicles.example.json rover-sim oa_test.py plot_run.py /gz/

ENV GZ_SIM_SYSTEM_PLUGIN_PATH=/opt/ardupilot_gazebo/lib/ardupilot_gazebo \
    GZ_SIM_RESOURCE_PATH=/gz/models:/opt/ardupilot_gazebo/share/ardupilot_gazebo/models \
    MAVLINK20=1 MAVLINK_DIALECT=ardupilotmega PYTHONUNBUFFERED=1
