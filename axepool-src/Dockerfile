# AxePool - Stratum server container
# Stdlib-only Python; no pip install needed -> tiny, fast, arm64-friendly (Pi 5).
# Pinned to an exact image (the one CI resolved python:3.13-slim to on 2026-11-07) so a
# rebuild never silently changes the base. To pick up base-image security fixes, update
# the digest; the weekly scan workflow flags when that is needed.
FROM python:3.13-slim@sha256:09a17cad02668ae61ece136b4fd36ac3849e14f670ebedb637ea3e15580525bc

# Run as a non-root user for safety.
RUN useradd -r -u 10001 -m app

WORKDIR /app
COPY server/ /app/server/

ENV PYTHONUNBUFFERED=1

USER app

EXPOSE 3334 3335

# Health = the status service answering locally.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD python3 -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:3335/status', timeout=3)" || exit 1

CMD ["python3", "/app/server/stratum.py"]
