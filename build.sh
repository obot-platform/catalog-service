#!/bin/bash

# Build the frontend
cd frontend
npm run build

# Make sure the server has the frontend files
cd ..
mkdir -p frontend/dist

# Start the server
go run main.go 