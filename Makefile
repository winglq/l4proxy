GOPATH = $(shell go env GOPATH)
bin:
	go build -o l4proxy src/main.go

protoc:
	protoc --govalidators_out=$(GOPATH)/src -I$(GOPATH)/src/github.com/mwitkow --go_out=plugins=grpc:$(GOPATH)/src -I. proto/proxy.proto

win:
	GOOS=windows go build -o l4proxy.exe src/main.go

.phony: l4proxy l4proxy.exe
