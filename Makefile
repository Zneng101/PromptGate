.PHONY: build web go run clean dev-web dev-go all release release-all

# 一键构建：前端 + 后端，产出单文件二进制
build: web go

# 构建前端（输出到 internal/web/static，供 Go embed）
web:
	cd webui && npm install && npm run build

# 构建后端二进制
go:
	go build -o promptgate ./cmd/promptgate

# Windows 下使用 promptgate.exe
go-windows:
	go build -o promptgate.exe ./cmd/promptgate

# 直接运行（开发）
run:
	go run ./cmd/promptgate

# 前端开发服务器（热更新，代理 /api 与 /v1 到本地 :8099）
dev-web:
	cd webui && npm run dev

# 后端开发服务器（Mock 模式，端口 8099）
dev-go:
	go run ./cmd/promptgate --port 8099

# ========== Release 跨平台构建 ==========

# 版本号：优先取 git tag，否则用 short commit
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DIST_DIR := dist

# 单平台构建：make release GOOS=linux GOARCH=amd64
release: web
	@mkdir -p $(DIST_DIR)
	@echo "==> 构建 $(GOOS)/$(GOARCH) v$(VERSION)"
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 \
		go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" \
		-o $(DIST_DIR)/promptgate-$(GOOS)-$(GOARCH)$(shell if [ "$(GOOS)" = "windows" ]; then echo .exe; fi) \
		./cmd/promptgate

# 一次性构建全平台，打包成压缩档
release-all: web
	@rm -rf $(DIST_DIR) && mkdir -p $(DIST_DIR)
	@for target in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64 windows-amd64; do \
		os=$${target%-*}; arch=$${target#*-}; \
		echo "==> 构建 $$os/$$arch v$(VERSION)"; \
		ext=""; if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 \
			go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" \
			-o $(DIST_DIR)/promptgate-$$target$$ext ./cmd/promptgate; \
	done
	@echo "==> 打包"
	@cd $(DIST_DIR) && for target in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64; do \
		tar -czf promptgate-$$target.tar.gz promptgate-$$target && rm -f promptgate-$$target; \
	done
	@cd $(DIST_DIR) && for target in windows-amd64; do \
		zip -q promptgate-$$target.zip promptgate-$$target.exe && rm -f promptgate-$$target.exe; \
	done
	@echo "==> 产物:" && ls -lh $(DIST_DIR)/

clean:
	rm -f promptgate promptgate.exe
	rm -rf data config.yaml internal/web/static/assets internal/web/static/index.html dist
