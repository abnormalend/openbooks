FROM node:16 AS web
WORKDIR /web
COPY . .
WORKDIR /web/server/app/
RUN npm install
RUN npm run build

FROM golang AS build
WORKDIR /go/src/
COPY . .
COPY --from=web /web/ .

ENV CGO_ENABLED=0
RUN go get -d -v ./...
RUN go install -v ./...
WORKDIR /go/src/cmd/openbooks/
RUN go build

FROM gcr.io/distroless/static AS app
WORKDIR /app
COPY --from=build /go/src/cmd/openbooks/openbooks .

EXPOSE 80
VOLUME [ "/books" ]
ENV BASE_PATH=/

ENTRYPOINT ["./openbooks", "server", "--dir", "/books", "--port", "80"]
