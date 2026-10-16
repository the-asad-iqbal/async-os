FROM debian:trixie

# Set shell
SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# Build tools
RUN apt-get update && apt-get install -y --no-install-recommends \
        automake \
        bash \
        bc \
        binutils \
        build-essential \
        bzip2 \
        ca-certificates \
        cpio \
        curl \
        e2fsprogs \
        file \
        git \
        graphviz \
        help2man \
        jq \
        make \
        ncurses-dev \
        openssh-client \
        patch \
        perl \
        pigz \
        python-is-python3 \
        python3 \
        python3-matplotlib \
        qemu-utils \
        rsync \
        sudo \
        texinfo \
        unzip \
        vim \
        wget \
        zip \
    && rm -rf /var/lib/apt/lists/*

# Init entry
COPY scripts/entry.sh /usr/sbin/
ENTRYPOINT ["/usr/sbin/entry.sh"]

# Get buildroot
WORKDIR /build
