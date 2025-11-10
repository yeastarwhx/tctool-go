#!/bin/bash
# Build tctool-go with all source files including extension management
# Source files: main.go, tunnel.go, sipmsg.go, pjsip.go, extension.go, ext_parser.go
go build -o tctool-go .
