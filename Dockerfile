FROM golang:1.27.2-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /kintsugi ./cmd/kintsugi

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /kintsugi /kintsugi
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/kintsugi"]
CMD ["serve"]
