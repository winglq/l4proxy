GOPATH = $(shell go env GOPATH)
bin:
	go build -o l4proxy src/main.go
	go build -o redirect src/redirect/main.go

protoc:
	protoc --govalidators_out=$(GOPATH)/src -I$(GOPATH)/src/github.com/mwitkow --go_out=plugins=grpc:$(GOPATH)/src -I. proto/proxy.proto

win:
	GOOS=windows go build -o l4proxy.exe src/main.go

rasp:
	GOOS=linux GOARCH=arm GOARM=7 go build -o l4proxy_rasp src/main.go

lib:
	ANDROID_HOME=/root/Android/Sdk/ ANDROID_NDK_HOME=/root/Android/Sdk/ndk/21.0.6113669 gomobile bind -target android github.com/winglq/l4proxy/src/client/forwarder

.phony: l4proxy l4proxy.exe
