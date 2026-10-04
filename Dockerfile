FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /easyupdate .

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=build /easyupdate /app/easyupdate
COPY config.example.yaml /app/config.example.yaml
EXPOSE 8080
ENTRYPOINT ["/app/easyupdate"]
