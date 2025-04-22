FROM node:18-alpine AS frontend

WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm install
COPY frontend ./
RUN npm run build

FROM golang:1.24-alpine AS backend

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

COPY --from=frontend /app/frontend/dist ./frontend/dist

RUN go build -o app main.go

FROM alpine:latest

WORKDIR /app
COPY --from=backend /app/app .
COPY --from=backend /app/frontend/dist ./frontend/dist

EXPOSE 8080

CMD ["./app"]
