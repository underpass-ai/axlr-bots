# The rave's service in a container: Python (standard library) for the
# clock and the chairs, Node for Strudel and superdough headless, the drum
# samples fetched at build time so the pod needs no network (--no-network).
# The page is the orchestra's (score-service/static). Build from
# examples/orchestra:
#
#   podman build --platform linux/amd64 -f rave-service/Dockerfile -t <registry>/axlr-bots/orchestra-rave:<tag> .
FROM docker.io/library/node:22.23.4-trixie-slim@sha256:2117f713a417f3aff682ac8f5bc2ebbdf1beed51f36e67ab1e0fd3866b6d2d8f
RUN apt-get update \
 && apt-get install -y --no-install-recommends python3 ca-certificates libasound2t64 \
 && rm -rf /var/lib/apt/lists/*
COPY rave-service/strudel/package.json rave-service/strudel/package-lock.json /app/rave-service/strudel/
RUN cd /app/rave-service/strudel && npm ci --omit=dev --no-audit --no-fund && npm cache clean --force
COPY score-service/static/ /app/score-service/static/
COPY rave-service/ /app/rave-service/
# The samples the rave names, once, into the image: sound.mjs warms them and
# says READY; the cache is what the pod reads.
RUN rm -rf /app/rave-service/demo /app/rave-service/__pycache__ /app/rave-service/Dockerfile \
 && cd /app/rave-service/strudel \
 && (timeout 120 node --import ./register.mjs sound.mjs --cache /opt/rave/samples | head -c 1 >/dev/null || true) \
 && test "$(ls /opt/rave/samples | wc -l)" -ge 20 \
 && chmod -R a+rX /opt/rave
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 HOME=/tmp
USER 65532:65532
WORKDIR /app/rave-service
EXPOSE 8794
ENTRYPOINT ["python3", "/app/rave-service/server.py"]
