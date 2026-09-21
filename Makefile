# Pi Teacher 业务 CLI 开发任务.
#
# 项目要求 Go 1.26.7; GOTOOLCHAIN 让旧版本本地工具链自动拉取指定版本,
# 避免每个开发者手工管理多套 Go 安装.

BINARY  := pi-teacher
CMD     := ./cmd/pi-teacher
BIN_DIR := bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/Pi-Teacher/cli/internal/version.Version=$(VERSION)

.PHONY: help build test test-race lint fmt vet staticcheck clean tidy

help: ## 显示帮助.
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

build: ## 构建 CLI 二进制到 ./bin.
	GOTOOLCHAIN=go1.26.7 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(CMD)

test: ## 运行全部测试.
	GOTOOLCHAIN=go1.26.7 go test ./...

test-race: ## 带竞态检测运行测试.
	GOTOOLCHAIN=go1.26.7 go test -race ./...

lint: fmt vet ## 格式化并静态检查.

fmt: ## 格式化全部 Go 源码.
	GOTOOLCHAIN=go1.26.7 gofmt -w .

vet: ## 运行 go vet.
	GOTOOLCHAIN=go1.26.7 go vet ./...

staticcheck: ## 运行 staticcheck.
	GOTOOLCHAIN=go1.26.7 go run honnef.co/go/tools/cmd/staticcheck@latest ./...

tidy: ## 整理 go.mod/go.sum.
	GOTOOLCHAIN=go1.26.7 go mod tidy

clean: ## 清理构建产物.
	rm -rf $(BIN_DIR)
