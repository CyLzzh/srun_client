go mod tidy
set GOOS=linux
set GOARCH=arm64
go build -ldflags="-s -w" -o srunloginarm64  .
pause