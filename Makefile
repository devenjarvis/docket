# Docket build targets. The desktop app needs cgo; on Linux it also needs the
# GTK4 and WebKitGTK 6.0 development packages (libgtk-4-dev libwebkitgtk-6.0-dev).

.PHONY: app serve demo test vet

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
