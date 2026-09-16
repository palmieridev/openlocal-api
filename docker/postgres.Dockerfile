FROM postgres:17-alpine
# Pin the upstream source, not a floating queue image; retain our Postgres major.
RUN apk add --no-cache --virtual .pgmq-build git make \
    && git clone https://github.com/pgmq/pgmq.git /tmp/pgmq \
    && cd /tmp/pgmq \
    && git checkout 9bb5ba79b30195e91e676d53e914fdc618ee41cd \
    && make -C pgmq-extension install \
    && rm -rf /tmp/pgmq \
    && apk del .pgmq-build
