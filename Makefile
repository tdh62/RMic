.PHONY: build build-gui check-gui package-gui package-gui-windows test check test-native test-browser package package-windows package-macos clean

build:
	go build -trimpath -ldflags='-s -w' -o bin/rmic ./cmd/rmic

build-gui:
	go -C desktop build -trimpath -ldflags='-s -w' -o ../bin/rmic-gui ./cmd/rmic-gui

check-gui:
	bash .ci/check-gui.sh

package-gui:
	bash .ci/package.sh linux "$$(go env GOHOSTARCH)" gui

package-gui-windows:
	bash .ci/package.sh windows amd64 gui

test:
	go test ./...

check:
	go vet ./...
	go test -race ./...

test-native:
	RMIC_NATIVE_TEST=1 go test -race ./internal/transport

test-browser: build
	npm --prefix tools/browser test

package:
	bash .ci/package.sh

package-windows:
	bash .ci/package.sh windows amd64

package-macos:
	bash .ci/package.sh darwin

clean:
	rm -rf bin artifacts dist
