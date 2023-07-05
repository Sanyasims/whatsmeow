# syntax=docker/dockerfile:1
FROM golang:1.20
RUN apt-get update && apt-get install -y build-essential

# Set destination for COPY
RUN mkdir /app
WORKDIR /app

RUN export G0111MODULE=on

#RUN cd /app

COPY ./ ./whatsmeow

RUN cd /app/whatsmeow/webtest && go build

WORKDIR /app/whatsmeow/webtest

ENTRYPOINT "/app/whatsmeow/webtest/webtest"
