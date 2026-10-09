# The recordings in testdata/ were made by the real recorders, in this
# container, so the escape sequences in them are the ones bash, zsh,
# readline and each recorder actually write. Nobody's real machine is in
# them: the user is "demo" and the host is "lab".
FROM debian:stable-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends python3 python3-venv zsh ttyrec bsdutils ca-certificates curl nodejs npm \
 && rm -rf /var/lib/apt/lists/*
RUN python3 -m venv /opt/asciinema2 && /opt/asciinema2/bin/pip install -q asciinema==2.4.0
RUN python3 -m venv /opt/termtosvg && /opt/termtosvg/bin/pip install -q termtosvg==1.1.0
RUN npm install -g --silent svg-term-cli@2.1.1
RUN curl -sSL -o /usr/local/bin/asciinema3 \
      https://github.com/asciinema/asciinema/releases/download/v3.2.1/asciinema-x86_64-unknown-linux-gnu \
 && chmod +x /usr/local/bin/asciinema3
RUN useradd -m -s /bin/bash demo && cp /etc/skel/.bashrc /home/demo/.bashrc
COPY drive.py /usr/local/bin/drive.py
USER demo
WORKDIR /home/demo
ENV TERM=xterm-256color LANG=C.UTF-8 USER=demo
