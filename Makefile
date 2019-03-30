all:
	go build

release:
	go build -ldflags '-s -w'

# This produces a small dynamic binary that depends on the huge libgo.so.
# It will probably not run as is on another Linux machine, because
# gccgo package needs to be installed first.
gcc:
	gccgo -o tdu_gccgo tdu.go

# Cross compile for Windows
win: tdu_windows.go
	GOOS=windows go build -ldflags '-s -w'

