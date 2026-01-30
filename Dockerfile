FROM ubuntu:24.04

# Install dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    tcpdump \
    bash \
    util-linux \
    && rm -rf /var/lib/apt/lists/*

# Copy the compiled binary
COPY simple-packet-capture /usr/local/bin/simple-packet-capture

ENTRYPOINT ["/usr/local/bin/simple-packet-capture"]
