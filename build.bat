@echo off
echo build...
set GOARCH=386
go build -ldflags "-w -s"
upx distrDownload.exe
