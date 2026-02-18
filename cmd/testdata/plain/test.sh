#!/bin/bash

# Simple test script

echo "Running test..."

# Example test: check if a file exists
TEST_FILE="sample.txt"

if [ -f "${BLA_BLUB | md5}" ]; then
    echo "Test passed: ${FLOAT} exists."
    exit 0
else
    echo "Test failed: ${SPECIAL_CHARACTERS} does not exist."
    exit 1
fi