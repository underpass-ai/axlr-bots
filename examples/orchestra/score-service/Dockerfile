# The orchestra's score service in a container: Python, FluidSynth and a
# General MIDI soundfont (FluidR3_GM, MIT). Build from examples/orchestra:
#
#   podman build --platform linux/amd64 -f score-service/Dockerfile -t <registry>/axlr-bots/orchestra-score:<tag> .
#
# It listens on loopback unless it is given its names (server.py
# --public-host, --internal-host): examples/helm/orchestra does.
FROM docker.io/library/python:3.13-slim@sha256:d98cc29d3266e535dd9da1099688d9cb3ca4753813571e071763d17725460962
RUN apt-get update \
 && apt-get install -y --no-install-recommends libfluidsynth3 fluid-soundfont-gm \
 && rm -rf /var/lib/apt/lists/*
COPY score-service/requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir -r /tmp/requirements.txt && rm /tmp/requirements.txt
COPY score-service/ /app/
RUN rm -rf /app/__pycache__ /app/test_*.py /app/Dockerfile \
 && test -f /usr/share/sounds/sf2/FluidR3_GM.sf2
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 HOME=/tmp
USER 65532:65532
WORKDIR /app
EXPOSE 8790
ENTRYPOINT ["python", "/app/server.py"]
