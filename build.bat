@echo off
set VERSION=1.0.1
wails build -ldflags "-X main.version=%VERSION%"