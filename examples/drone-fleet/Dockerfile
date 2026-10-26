# The port fleet's services: vehicle_pilot.py beside each vehicle and
# fleet_base.py (stations, stock, the live map). Python's standard library
# and pymavlink, nothing else.
FROM docker.io/library/python:3.13-slim@sha256:d98cc29d3266e535dd9da1099688d9cb3ca4753813571e071763d17725460962 AS build
RUN pip wheel --no-cache-dir --wheel-dir /wheels pymavlink==2.4.50

FROM docker.io/library/python:3.13-slim@sha256:d98cc29d3266e535dd9da1099688d9cb3ca4753813571e071763d17725460962
COPY --from=build /wheels /wheels
RUN pip install --no-index --no-cache-dir /wheels/*.whl && rm -rf /wheels
COPY app/ /app/
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 MAVLINK20=1 MAVLINK_DIALECT=ardupilotmega
USER 65532:65532
WORKDIR /app
CMD ["python", "/app/vehicle_pilot.py"]
