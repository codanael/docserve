FROM alpine:3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659 AS certs
RUN apk add --no-cache ca-certificates

FROM scratch

LABEL org.opencontainers.image.source=https://github.com/codanael/docserve
LABEL org.opencontainers.image.description="Self-hosted MCP documentation server. Fetches docs from Git providers, indexes with SQLite FTS5, serves to LLM agents via MCP Streamable HTTP."
LABEL org.opencontainers.image.licenses=MIT
ARG TARGETPLATFORM
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY ${TARGETPLATFORM}/docserve /docserve
ENTRYPOINT ["/docserve"]
