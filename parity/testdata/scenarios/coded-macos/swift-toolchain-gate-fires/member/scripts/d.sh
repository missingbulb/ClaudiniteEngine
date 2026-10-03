#!/bin/bash
# xcode-select -p
if which /usr/bin/swiftc; then :; fi
command -v \
  swift
xcode-select -p || exit 0
command -v swift
