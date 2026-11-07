# AxePool - Stratum server container
# Stdlib-only Python; no pip install needed -> tiny, fast, arm64-friendly (Pi 5).
# Pinned to an exact image (the one CI resolved python:3.13-slim to on 2026-11-07) so a
# rebuild never silently changes the base. Debian security updates are applied below at
# build time; the weekly scan workflow flags when a rebuild is needed.
FROM python:3.13-slim@sha256:09a17cad02668ae61ece136b4fd36ac3849e14f670ebedb637ea3e15580525bc

# 1) Debian security updates published after the pinned base (the build's Trivy scan
#    blocks any fixable HIGH/CRITICAL left over).
# 2) No pip/setuptools: the server is stdlib-only and installs nothing, so the package
#    tooling (and the libraries it vendors) is attack surface with no job here.
RUN set -eux; \
    apt-get update; \
    apt-get upgrade -y --no-install-recommends; \
    rm -rf /var/lib/apt/lists/*; \
    rm -rf /usr/local/lib/python3.13/site-packages/pip \
           /usr/local/lib/python3.13/site-packages/pip-*.dist-info \
           /usr/local/lib/python3.13/site-packages/setuptools \
           /usr/local/lib/python3.13/site-packages/setuptools-*.dist-info \
           /usr/local/lib/python3.13/site-packages/pkg_resources \
           /usr/local/lib/python3.13/site-packages/_distutils_hack \
           /usr/local/lib/python3.13/ensurepip \
           /usr/local/bin/pip /usr/local/bin/pip3 /usr/local/bin/pip3.13; \
    ! python3 -c "import pip" 2>/dev/null

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
