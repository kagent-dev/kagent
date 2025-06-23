DOC_IMG=cr.kagent.dev/kagent-dev/kagent/tools-docs:local

build:
	bun install
	npm audit fix

build-docker: build
	docker buildx build --load -t  $(DOC_IMG) .

run: build-docker
	docker run --rm -t -e OPENAI_API_KEY=$OPENAI_API_KEY -p 3001:3001 $(DOC_IMG)