VERSION ?= 0.0.1-dev
# Adresse des BetterNAS-APT-Repos, die ins Image eingetragen wird (leer = keine BetterNAS-Quelle)
APT_URL ?=
# Update-Kanal der ins Image eingetragenen Quelle (stable oder testing)
APT_SUITE ?= stable
ARCH    ?= amd64
LDFLAGS := -s -w -X github.com/betternas/betternas/internal/version.Version=$(VERSION)
BUILDER := betternas-builder
DOCKER_RUN := docker run --rm --privileged --platform linux/$(ARCH) -e APT_URL=$(APT_URL) -e APT_SUITE=$(APT_SUITE) -v $(CURDIR):/src -w /src $(BUILDER)-$(ARCH)

.PHONY: test test-go test-web test-tools web build build-arm64 builder rootfs iso e2e deb deploy clean apt-key repo dev-repo

test: test-go test-web test-tools

test-go:
	cd backend && go vet ./... && go test ./...

test-web:
	cd frontend && npm test

# Shell-Tests der Release-Werkzeuge (brauchen dpkg, apt-ftparchive, gpg): auf Linux direkt, sonst im Builder-Container
test-tools:
	@if [ "$$(uname)" = Linux ]; then tools/tests/run.sh; else $(DOCKER_RUN) tools/tests/run.sh; fi

web:
	cd frontend && npm ci --no-audit --no-fund && npm run build

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

deb:
	$(DOCKER_RUN) packaging/build-deb.sh $(ARCH) $(VERSION)

# Einmalig: Signierschlüssel für das APT-Repo erzeugen (privater Schlüssel nach ~/.betternas/, selbst sichern!)
apt-key:
	tools/apt-key.sh

# Signiertes APT-Repo aus den gebauten Paketen nach dist/apt (zum Hochladen auf den Webserver)
repo:
	tools/apt-repo.sh dist/apt $(wildcard dist/betternas_$(VERSION)_*.deb)

# Entwicklungs-Repo auf dem Entwicklungsrechner veröffentlichen und als APT-Quelle eintragen
dev-repo:
	tools/dev-repo.sh $(VM) $(VERSION)

VM ?= betternas-dev
deploy:
	scp dist/betternas_$(VERSION)_$(ARCH).deb $(VM):/tmp/
	ssh $(VM) 'sudo apt-get install -y --allow-downgrades /tmp/betternas_$(VERSION)_$(ARCH).deb && rm /tmp/betternas_$(VERSION)_$(ARCH).deb'

# Nativer Build und KVM-Tests auf dem Entwicklungsrechner, z. B. make remote VERSION=0.0.3-dev MODES="uefi bios"
REMOTE ?= betternas-dev
MODES ?= uefi
remote:
	tools/remote-build.sh $(REMOTE) $(VERSION) $(MODES)

# Echte Storage-Integrationstests (root, Loop-Geräte) auf dem Entwicklungsrechner
integration:
	cd backend && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -tags integration -o ../bin/integration.test ./internal/agent
	scp bin/integration.test $(REMOTE):/tmp/
	ssh $(REMOTE) 'sudo /tmp/integration.test -test.run "TestStorageLifecycle|TestAccountsLifecycle|TestSharesLifecycle|TestSnapraidLifecycle|TestDockerLifecycle|TestDockerOps|TestVMLifecycle|TestVMConfigureLibvirt|TestVMTuningLibvirt|TestVMDiskAddRemoveLibvirt|TestVMRenameLibvirt|TestVMLiveLibvirt|TestVMSharedDirsLibvirt|TestVMOps|TestSmartAndSpindown|TestSnapraidReplaceFix|TestNfsExports" -test.v; rc=$$?; rm -f /tmp/integration.test; exit $$rc'
