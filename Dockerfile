FROM golang:1.22 AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o saas-starter .

FROM gcr.io/distroless/static-debian12
COPY --from=build /app/saas-starter /saas-starter
EXPOSE 8080
ENTRYPOINT ["/saas-starter"]
