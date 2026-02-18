#!/bin/bash

# Simple test script

echo "Running test..."

# Example test: check if a file exists
TEST_FILE="sample.txt"

if [ -f "${TEST_FILE | md5}" ]; then
    echo "Test passed: ${TEST_FILE} exists."
    exit 0
else
    echo "Test failed: ${TEST_FILE} does not exist."
    exit 1
fi