# Mobius agent sandbox image.
#
# Commands issued by agents (run_command, the bash adapter) execute inside an
# ephemeral container built from this image. The only writable mount is the
# project (or an ephemeral temp dir), so a runaway command cannot damage the
# host outside that directory.
#
# Bundles the build toolchains agents commonly need: Python, Go, Rust, C/C++,
# and Java. Go and Rust track the latest stable release at build time; Python,
# Java and the C/C++ toolchain come from Debian stable. Rebuild to upgrade:
#
#   make build-sandbox
#
FROM debian:trixie-slim

ENV DEBIAN_FRONTEND=noninteractive \
    PATH=/usr/local/go/bin:/root/.cargo/bin:/usr/local/bin:/usr/bin:/bin

# Base utilities + C/C++ + Python + Java (latest stable from Debian trixie).
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        ca-certificates curl wget git make pkg-config xz-utils \
        build-essential clang cmake gdb \
        python3 python3-pip python3-venv python3-dev \
        default-jdk \
        ripgrep jq unzip ffmpeg; \
    rm -rf /var/lib/apt/lists/*

# Go — latest stable, resolved at build time from the official release feed.
RUN set -eux; \
    GOVER="$(curl -fsSL https://go.dev/VERSION?m=text | head -n1)"; \
    ARCH="$(dpkg --print-architecture)"; \
    curl -fsSL "https://go.dev/dl/${GOVER}.linux-${ARCH}.tar.gz" -o /tmp/go.tgz; \
    tar -C /usr/local -xzf /tmp/go.tgz; \
    rm /tmp/go.tgz; \
    go version

# Rust — latest stable via rustup (minimal profile).
RUN set -eux; \
    curl --proto '=https' --tlsv1.2 -fsSL https://sh.rustup.rs \
        | sh -s -- -y --default-toolchain stable --profile minimal; \
    rustc --version

ENV PLAYWRIGHT_BROWSERS_PATH=/opt/pw-browsers \
    NODE_PATH=/usr/lib/node_modules

# Node.js (v20) and Playwright Chromium dependencies
RUN set -eux; \
    curl -fsSL https://deb.nodesource.com/setup_20.x | bash -; \
    apt-get install -y --no-install-recommends nodejs; \
    npx playwright install-deps chromium; \
    npm install -g playwright; \
    npx playwright install chromium; \
    chmod -R 755 /opt/pw-browsers; \
    rm -rf /var/lib/apt/lists/*

COPY bin/infinishield /usr/local/bin/infinishield
RUN chmod +x /usr/local/bin/infinishield

WORKDIR /work
