#!/bin/bash
# Build tctool-go with all source files including extension management
# Source files: main.go, tunnel.go, sipmsg.go, sip_parser.go, extension.go, ext_parser.go
# Using pure Go SIP implementation (sipgo) - no CGO dependencies

echo "Building tctool-go..."
go build -o tctool-go .

if [ $? -eq 0 ]; then
    echo "Build successful!"
    echo "Executable: tctool-go"
else
    echo "Build failed!"
    exit 1
fi
