all:
	go build

release:
	go build -ldflags '-s -w'

