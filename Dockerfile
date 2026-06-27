# Used by GoReleaser (dockers_v2): the prebuilt binary is copied from the
# multi-arch build context at $TARGETPLATFORM/janusmcp — do NOT build it here.
# Node is included so upstream MCP servers launched via `npx` (e.g. Supabase) work.
FROM alpine:3.20

ARG TARGETPLATFORM

RUN apk add --no-cache ca-certificates nodejs npm

COPY $TARGETPLATFORM/janusmcp /usr/bin/janusmcp

# HTTP transport by default in containers (override with JANUS_TRANSPORT).
ENV JANUS_TRANSPORT=http \
    JANUS_HTTP_HOST=0.0.0.0 \
    JANUS_HTTP_PORT=7332
EXPOSE 7332

ENTRYPOINT ["janusmcp"]
CMD ["serve"]
