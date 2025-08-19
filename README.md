# mysql-tunnel
A HTTP tunnel for mysql

## php
需要 8.X

## python
需要 3.X

## go

### 直接运行
go run navicat_tunnel.go

### 编译运行
go build -o navicat_tunnel navicat_tunnel.go
./navicat_tunnel

#### Linux
GOOS=linux GOARCH=amd64 go build -o navicat_tunnel_linux navicat_tunnel.go

#### Windows
GOOS=windows GOARCH=amd64 go build -o navicat_tunnel.exe navicat_tunnel.go

#### macOS
GOOS=darwin GOARCH=amd64 go build -o navicat_tunnel_mac navicat_tunnel.go