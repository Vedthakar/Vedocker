.PHONY: build cli daemon ui clean

build: cli daemon

cli:
	go build -o minicontainer .

daemon:
	go build -o minicontainerd ./cmd/minicontainerd

ui:
	cd minicontainer-ui && npm install && npm run dev

clean:
	rm -f minicontainer minicontainerd
