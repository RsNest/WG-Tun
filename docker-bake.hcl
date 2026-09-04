variable "GO_IMAGE" { default = "golang:1.26.8-bookworm" }
variable "RUNTIME_IMAGE" { default = "debian:bookworm-slim" }
variable "VERSION" { default = "dev" }
variable "COMMIT" { default = "unknown" }
variable "CREATED" { default = "" }
variable "SOURCE" { default = "https://github.com/RsNest/WG-Tun" }
variable "REGISTRY" { default = "ghcr.io/rsnest" }

target "_common" {
  context = "."
  dockerfile = "Dockerfile"
  args = {
    GO_IMAGE      = GO_IMAGE
    RUNTIME_IMAGE = RUNTIME_IMAGE
    VERSION       = VERSION
    COMMIT        = COMMIT
    CREATED       = CREATED
    SOURCE        = SOURCE
  }
}

target "test" {
  inherits = ["_common"]
  target   = "test"
}

target "controller" {
  inherits = ["_common"]
  target   = "controller"
  tags     = ["${REGISTRY}/wg-tun-controller:local"]
  platforms = ["linux/amd64"]
}

target "agent" {
  inherits = ["_common"]
  target   = "agent"
  tags     = ["${REGISTRY}/wg-tun-agent:local"]
  platforms = ["linux/amd64"]
}

target "proxctl" {
  inherits = ["_common"]
  target   = "proxctl"
  tags     = ["${REGISTRY}/wg-tun-proxctl:local"]
  platforms = ["linux/amd64"]
}

group "default" {
  targets = ["controller", "agent", "proxctl"]
}

group "validate" {
  targets = ["test", "controller", "agent", "proxctl"]
}
