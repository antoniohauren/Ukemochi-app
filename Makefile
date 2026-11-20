TAILWIND_VERSION := v4.1.13
TAILWIND := bin/tailwindcss
HTMX_VERSION := 2.0.4
HTMX := static/htmx.min.js
HTMX_SHA256 := e209dda5c8235479f3166defc7750e1dbcd5a5c1808b7792fc2e6733768fb447

UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)
TAILWIND_OS := $(if $(filter Darwin,$(UNAME_S)),macos,linux)
TAILWIND_ARCH := $(if $(filter arm64 aarch64,$(UNAME_M)),arm64,x64)
TAILWIND_ASSET := tailwindcss-$(TAILWIND_OS)-$(TAILWIND_ARCH)
TAILWIND_URL := https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)

.PHONY: gen run test build-arm64

$(TAILWIND):
	mkdir -p bin
	curl -fsSL -o bin/$(TAILWIND_ASSET) $(TAILWIND_URL)/$(TAILWIND_ASSET)
	curl -fsSL -o bin/sha256sums.txt $(TAILWIND_URL)/sha256sums.txt
	cd bin && grep " \(\./\)\?$(TAILWIND_ASSET)$$" sha256sums.txt | sha256sum -c -
	mv bin/$(TAILWIND_ASSET) $@
	chmod +x $@

$(HTMX):
	curl -fsSL -o $@.tmp https://unpkg.com/htmx.org@$(HTMX_VERSION)/dist/htmx.min.js
	echo "$(HTMX_SHA256)  $@.tmp" | sha256sum -c - || { rm -f $@.tmp; exit 1; }
	mv $@.tmp $@

gen: $(TAILWIND) $(HTMX)
	go tool templ generate
	$(TAILWIND) -i web/input.css -o static/app.css --minify

run: gen
	go run ./cmd/ukemochi

test: gen
	go test ./...

build-arm64: gen
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o bin/ukemochi-arm64 ./cmd/ukemochi
