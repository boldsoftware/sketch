#!/bin/bash

# Build the sketch binary
go build -o sketch ./cmd/sketch

# Run sketch with Kali Linux as the base image
./sketch -base-image="kalilinux/kali-rolling:latest" "$@"