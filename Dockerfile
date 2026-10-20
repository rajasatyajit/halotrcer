# The release image: halotrcer with tokvera and sievant beside it (built by
# GoReleaser's dockers_v2 from the binary it has already built; nothing is
# compiled here).
#
# tokvera and sievant are copied out of their own released images, never
# rebuilt: TOKVERA_IMAGE and SIEVANT_IMAGE are image@sha256 references from
# packaging/bundled-tools.env (scripts/release/bundled-tools.sh), each
# cosign-verified against its tool's release workflow before this build.
# They have no default, so an unpinned build fails at FROM. Both images put
# their binary at /usr/bin/<tool> and their LICENSE and NOTICE under
# /usr/share/doc/<tool>/ (their Dockerfiles); for each platform, BuildKit
# takes that platform's image from their multi-arch indexes.
#
# The base is distroless static, Debian 13, :nonroot, pinned by its
# multi-arch index digest: the digest both tools' release images use. Its
# config sets user 65532 (nonroot) on linux/amd64 and linux/arm64.
ARG TOKVERA_IMAGE
ARG SIEVANT_IMAGE
FROM ${TOKVERA_IMAGE} AS tokvera
FROM ${SIEVANT_IMAGE} AS sievant

FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3

ARG TARGETPLATFORM
COPY $TARGETPLATFORM/halotrcer /usr/bin/halotrcer
COPY LICENSE NOTICE /usr/share/doc/halotrcer/
COPY --from=tokvera /usr/bin/tokvera /usr/bin/tokvera
COPY --from=tokvera /usr/share/doc/tokvera/ /usr/share/doc/tokvera/
COPY --from=sievant /usr/bin/sievant /usr/bin/sievant
COPY --from=sievant /usr/share/doc/sievant/ /usr/share/doc/sievant/

USER 65532:65532
ENTRYPOINT ["/usr/bin/halotrcer"]
