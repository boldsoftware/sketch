#!/bin/bash

# Build the sketch binary
go build -o kifaru ./cmd/sketch

# Run sketch with Kali Linux as the base image
./kifaru -base-image="kalilinux/kali-rolling:latest" "$@"