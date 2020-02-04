bin:
	go build -o l4proxy src/main.go

protoc:
	protoc --go_out=plugins=grpc:/root/go/src -I. proto/proxy.proto

win:
	GOOS=windows go build -o l4proxy.exe src/main.go

.phony: l4proxy l4proxy.exe
