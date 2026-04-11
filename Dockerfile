FROM alpine:3 AS certs
RUN apk add --no-cache ca-certificates

FROM scratch
ARG TARGETPLATFORM
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY ${TARGETPLATFORM}/docserve /docserve
ENTRYPOINT ["/docserve"]
