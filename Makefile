$(shell git fetch --tags)
BUILD_DIR ?= $(dir $(realpath -s $(firstword $(MAKEFILE_LIST))))/build
VERSION ?= $(shell git describe --tags --always --dirty)
PREV_VERSION ?= $(shell git describe --abbrev=0 --tags `git rev-list --tags --skip=1 --max-count=1`)
GOOS ?= $(shell uname | tr '[:upper:]' '[:lower:]')
GOARCH ?= $(shell [[ `uname -m` = "x86_64" ]] && echo "amd64" || echo "arm64" )
GOPROXY ?= "https://proxy.golang.org|direct"
KDM_DOCKER_REPO ?= ${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com
KDM_IAM_ROLE_ARN ?= arn:aws:iam::${AWS_ACCOUNT_ID}:role/${CLUSTER_NAME}-k8s-dns-monitor

$(shell mkdir -p ${BUILD_DIR})

.PHONY: all
all: fmt verify test build

.PHONY: goreleaser
goreleaser: ## Release snapshot
	goreleaser build --snapshot --rm-dist

.PHONY: build
build: generate ## Build the controller image
	$(eval CONTROLLER_IMG=$(shell $(WITH_GOFLAGS) ko build -B --platform=$(TARGET_PLATFORMS) -t $(VERSION) github.com/bwagner5/k8s-dns-monitor/cmd/k8s-dns-monitor))
	$(eval CONTROLLER_TAG=$(shell echo ${CONTROLLER_IMG} | sed 's/.*k8s-dns-monitor://' | cut -d'@' -f1))
	$(eval CONTROLLER_DIGEST=$(shell echo ${CONTROLLER_IMG} | sed 's/.*k8s-dns-monitor:.*@//'))
	echo Built ${CONTROLLER_IMG}

apply-both: apply-coredns apply-rpd ## Apply both coredns and rpd canary

apply-coredns: build ## Deploy the controller from the current state of your git repository into your ~/.kube/config cluster
	helm upgrade --install k8s-dns-monitor-coredns charts/k8s-dns-monitor-chart --namespace k8s-dns-monitor-coredns --create-namespace \
	$(HELM_OPTS) \
	-f charts/k8s-dns-monitor-chart/values-coredns.yaml \
	--set serviceAccount.annotations.eks\\.amazonaws\\.com/role-arn=${KDM_IAM_ROLE_ARN}-coredns \
	--set image.repository=$(KO_DOCKER_REPO)/k8s-dns-monitor \
	--set image.digest="$(CONTROLLER_DIGEST)"

apply-rpd: build ## Deploy the controller from the current state of your git repository into your ~/.kube/config cluster
	helm upgrade --install k8s-dns-monitor-rpd charts/k8s-dns-monitor-chart --namespace k8s-dns-monitor-rpd --create-namespace \
	$(HELM_OPTS) \
	-f charts/k8s-dns-monitor-chart/values-rpd.yaml \
	--set serviceAccount.annotations.eks\\.amazonaws\\.com/role-arn=${KDM_IAM_ROLE_ARN}-rpd \
	--set image.repository=$(KO_DOCKER_REPO)/k8s-dns-monitor \
	--set image.digest="$(CONTROLLER_DIGEST)"

publish: verify build docs ## Build and publish container images and helm chart
	aws ecr-public get-login-password --region us-east-1 | docker login --username AWS --password-stdin ${KO_DOCKER_REPO}
	sed -i.bak "s|repository:.*|repository: $(KO_DOCKER_REPO)/k8s-dns-monitor|" charts/k8s-dns-monitor-chart/values.yaml
	sed -i.bak "s|tag:.*|tag: ${CONTROLLER_TAG}|" charts/k8s-dns-monitor-chart/values.yaml
	sed -i.bak "s|digest:.*|digest: ${CONTROLLER_DIGEST}|" charts/k8s-dns-monitor-chart/values.yaml
	sed -i.bak "s|version:.*|version: $(shell echo ${CONTROLLER_TAG} | tr -d 'v')|" charts/k8s-dns-monitor-chart/Chart.yaml
	sed -i.bak "s|appVersion:.*|appVersion: $(shell echo ${CONTROLLER_TAG} | tr -d 'v')|" charts/k8s-dns-monitor-chart/Chart.yaml
	sed -E -i.bak "s|$(shell echo ${PREV_VERSION} | tr -d 'v' | sed 's/\./\\./g')([\"_/])|$(shell echo ${VERSION} | tr -d 'v')\1|g" README.md
	rm -f *.bak charts/k8s-dns-monitor-chart/*.bak
	helm package charts/k8s-dns-monitor-chart -d ${BUILD_DIR_PATH} --version "${VERSION}"
	helm push ${BUILD_DIR_PATH}/k8s-dns-monitor-chart-${VERSION}.tgz "oci://${KO_DOCKER_REPO}"

docs: ## Generate helm docs
	helm-docs

.PHONY: test
test: ## run go tests and benchmarks
	go test -bench=. ${BUILD_DIR}/../... -v -coverprofile=coverage.out -covermode=atomic -outputdir=${BUILD_DIR}

.PHONY: version
version: ## Output version of local HEAD
	@echo ${VERSION}

.PHONY: verify
verify: licenses boilerplate ## Run Verifications like helm-lint and govulncheck
	govulncheck ./...
	golangci-lint run
	cd toolchain && go mod tidy

.PHONY: boilerplate
boilerplate: ## Add license headers
	go run hack/boilerplate.go ./

.PHONY: fmt
fmt: ## go fmt the code
	find . -iname "*.go" -exec go fmt {} \;

.PHONY: licenses
licenses: ## Verifies dependency licenses
	go mod download
	! go-licenses csv ./... | grep -v -e 'MIT' -e 'Apache-2.0' -e 'BSD-3-Clause' -e 'BSD-2-Clause' -e 'ISC' -e 'MPL-2.0'

.PHONY: update-readme
update-readme: ## Updates readme to refer to latest release
	sed -E -i.bak "s|$(shell echo ${PREV_VERSION} | tr -d 'v' | sed 's/\./\\./g')([\"_/])|$(shell echo ${VERSION} | tr -d 'v')\1|g" README.md
	rm -f *.bak

.PHONY: toolchain
toolchain:
	cd toolchain && go mod download && cat tools.go | grep _ | awk -F'"' '{print $$2}' | xargs -tI % go install %

.PHONY: generate
generate: ## Generate attribution
	# run generate twice, gen_licenses needs the ATTRIBUTION file or it fails.  The second run
	# ensures that the latest copy is embedded when we build.
	go generate ./...
	./hack/gen_licenses.sh
	go generate ./...

.PHONY: clean
clean: ## Clean artifacts
	rm -rf ${BUILD_DIR}
	rm -rf dist/

.PHONY: help
help: ## Display help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
