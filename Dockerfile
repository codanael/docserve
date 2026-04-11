FROM alpine:3 AS certs
RUN apk add --no-cache ca-certificates

ARG TARGETPLATFORM

FROM scratch
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY ${TARGETPLATFORM}/docserve /docserve
ENTRYPOINT ["/docserve"]
