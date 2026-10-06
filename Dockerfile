# syntax=docker/dockerfile:1

# The image holds nothing but the static tofu-plan-review binary. CI builds
# it with Nix and places it at dist/tofu-plan-review-<arch> (amd64, arm64)
# before building the image; there is no compiler or shell in here.
#
# GitHub runs Docker actions as root with the workspace as the working
# directory, so there is no USER or WORKDIR.
FROM scratch

LABEL org.opencontainers.image.source="https://github.com/tofu-contrib/tofu-plan-review" \
      org.opencontainers.image.description="Readable OpenTofu plan reviews on pull requests" \
      org.opencontainers.image.licenses="MPL-2.0"

ARG TARGETARCH
COPY --chmod=0755 dist/tofu-plan-review-${TARGETARCH} /tofu-plan-review

ENTRYPOINT ["/tofu-plan-review"]
CMD ["--help"]
