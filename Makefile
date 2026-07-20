.PHONY: all node apps wallet explorer clean

all: node apps wallet explorer

node:
	@echo "Building Node..."
	@cd node && if [ -f "go.mod" ]; then go build ./...; fi

apps:
	@echo "Building Apps..."
	@for dir in apps/*; do \
		if [ -d "$$dir/backend" ] && [ -f "$$dir/backend/go.mod" ]; then \
			echo "Building $$dir/backend (Go)..."; \
			cd "$$dir/backend" && go build ./... && cd ../../..; \
		fi; \
		if [ -d "$$dir/frontend" ] && [ -f "$$dir/frontend/package.json" ]; then \
			echo "Building $$dir/frontend (Node)..."; \
			cd "$$dir/frontend" && npm install && npm run build && cd ../../..; \
		fi; \
	done

wallet:
	@echo "Building Wallet..."
	@if [ -d "wallet" ]; then \
		if [ -f "wallet/go.mod" ]; then cd wallet && go build ./...; fi; \
		if [ -f "wallet/package.json" ]; then cd wallet && npm install && npm run build; fi; \
	fi

explorer:
	@echo "Building Explorer..."
	@if [ -d "explorer" ]; then \
		if [ -f "explorer/package.json" ]; then cd explorer && npm install && npm run build; fi; \
		if [ -f "explorer/go.mod" ]; then cd explorer && go build ./...; fi; \
	fi

clean:
	@echo "Cleaning up..."
	@find . -type f -name '*.o' -delete
	@find . -type d -name 'node_modules' -prune -exec rm -rf {} +
	@find . -type d -name 'dist' -prune -exec rm -rf {} +
