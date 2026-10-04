.PHONY: build cli daemon mcp ui extension test clean

build: cli daemon mcp

cli:
	go build -o minicontainer .

daemon:
	go build -o minicontainerd ./cmd/minicontainerd

mcp:
	go build -o vedocker-mcp ./cmd/vedocker-mcp

ui:
	cd minicontainer-ui && npm install && npm run dev

extension:
	./extension/scripts/package.sh

test:
	go test ./cmd/vedocker-mcp/...
	node --test extension/test/repo.test.js

clean:
	rm -f minicontainer minicontainerd vedocker-mcp
	rm -rf extension/dist
