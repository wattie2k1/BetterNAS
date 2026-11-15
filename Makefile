VERSION ?= 0.0.1-dev
ARCH    ?= amd64
LDFLAGS := -s -w -X github.com/betternas/betternas/internal/version.Version=$(VERSION)
BUILDER := betternas-builder
DOCKER_RUN := docker run --rm --privileged --platform linux/$(ARCH) -v $(CURDIR):/src -w /src $(BUILDER)-$(ARCH)

.PHONY: test build build-arm64 builder rootfs iso e2e clean

test:
	cd backend && go vet ./... && go test ./...

build:
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o ../bin/amd64/ ./cmd/...

build-arm64:
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o ../bin/arm64/ ./cmd/...

builder:
	docker build --platform linux/$(ARCH) -t $(BUILDER)-$(ARCH) -f image/Dockerfile.builder image

rootfs:
	$(DOCKER_RUN) image/build-rootfs.sh $(ARCH)
	$(DOCKER_RUN) image/tests/check-rootfs.sh dist/rootfs-$(ARCH).tar

iso:
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) go build -trimpath -ldflags "$(LDFLAGS)" -o ../bin/$(ARCH)/ ./cmd/betternas-installer
	$(DOCKER_RUN) image/build-iso.sh $(ARCH) $(VERSION)

e2e:
	test/image/e2e.sh dist/betternas-$(VERSION)-$(ARCH).iso $(MODE)

clean:
	rm -rf bin dist build
