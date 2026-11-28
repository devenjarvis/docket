# Docket build targets. The desktop app needs cgo; on Linux it also needs the
# GTK4 and WebKitGTK 6.0 development packages (libgtk-4-dev libwebkitgtk-6.0-dev).

.PHONY: app serve demo test vet icon

app: ## the desktop app (Wails 3)
	CGO_ENABLED=1 go build -tags desktop,production -o docket .

serve: ## the same UI in a browser, no Wails needed
	go run . serve

demo: ## sample pull requests and a scripted agent, in a browser
	go run . demo

test:
	go test ./...
	go vet -tags desktop ./...

vet:
	go vet ./...

icon: ## render the app icon from its SVG (needs rsvg-convert: librsvg)
	rsvg-convert -w 1024 -h 1024 internal/desktop/icon/appicon.svg -o internal/desktop/appicon.png
