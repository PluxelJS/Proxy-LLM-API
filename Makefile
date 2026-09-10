GO ?= go
TAGS := remote containers_image_openpgp
export CGO_ENABLED := 0

.PHONY: build web test integration ui-test check fmt
build:
	mkdir -p bin
	$(GO) build -trimpath -tags '$(TAGS)' -o bin/dev-runtime ./cmd/dev-runtime
web:
	npm ci --prefix web
	npm run build --prefix web
test:
	$(GO) test -tags '$(TAGS)' ./...
integration:
	DEV_RUNTIME_INTEGRATION=$(ENGINE) $(GO) test -tags '$(TAGS)' ./internal/app -run TestEngineIntegration -count=1 -v
ui-test: build
	npm run test --prefix web
check: test ui-test
fmt:
	$(GO) fmt ./...
	npm exec --prefix web -- prettier --write 'web/src/*'
