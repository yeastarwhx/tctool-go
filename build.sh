#!/bin/bash
# Build tctool-go with all source files including extension management
# Source files: main.go, tunnel.go, sipmsg.go, sip_parser.go, extension.go, ext_parser.go
# Using pure Go SIP implementation (sipgo) - no CGO dependencies
go build -o tctool-go .
